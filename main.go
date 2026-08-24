package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Aayush9029/OmaPad/internal/daemon"
	"github.com/Aayush9029/OmaPad/internal/ipc"
	"github.com/Aayush9029/OmaPad/internal/tui"
	"github.com/Aayush9029/OmaPad/internal/walkingpad"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omapad:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	socketPath := envOr("OMAPAD_SOCKET", ipc.DefaultSocketPath())
	for len(arguments) > 0 {
		switch {
		case arguments[0] == "--socket" && len(arguments) >= 2:
			socketPath, arguments = arguments[1], arguments[2:]
		case strings.HasPrefix(arguments[0], "--socket="):
			socketPath, arguments = strings.TrimPrefix(arguments[0], "--socket="), arguments[1:]
		default:
			goto parsedGlobals
		}
	}

parsedGlobals:

	command := "tui"
	if len(arguments) > 0 {
		command = arguments[0]
		arguments = arguments[1:]
	}

	if command == "--version" || command == "version" {
		fmt.Printf("omapad %s\n", version)
		return nil
	}
	if command == "help" || command == "--help" || command == "-h" {
		printHelp()
		return nil
	}
	if command == "daemon" {
		return runDaemon(arguments, socketPath)
	}
	client, err := ipc.New(socketPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch command {
	case "tui":
		return tui.Run(client)
	case "status":
		snapshot, err := client.State(ctx)
		if err != nil {
			return err
		}
		if hasFlag(arguments, "--json") {
			return printJSON(snapshot)
		}
		printStatus(snapshot)
		return nil
	case "watch":
		return watch(client, hasFlag(arguments, "--json"))
	case "start":
		state, err := client.State(ctx)
		if err != nil {
			return err
		}
		if state.IsRunning {
			printStatus(state)
			return nil
		}
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandStartPause}, hasFlag(arguments, "--json"))
	case "pause":
		state, err := client.State(ctx)
		if err != nil {
			return err
		}
		if !state.IsRunning {
			printStatus(state)
			return nil
		}
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandStartPause}, hasFlag(arguments, "--json"))
	case "stop":
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandStop}, hasFlag(arguments, "--json"))
	case "speed":
		if len(arguments) == 0 {
			return fmt.Errorf("usage: omapad speed <0.5-6.0>")
		}
		speed, err := strconv.ParseFloat(arguments[0], 64)
		if err != nil || speed < 0.5 || speed > 6 {
			return fmt.Errorf("speed must be between 0.5 and 6.0 km/h")
		}
		speed = float64(int(speed*10+0.5)) / 10
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandSetSpeed, Speed: &speed}, hasFlag(arguments, "--json"))
	case "mode":
		if len(arguments) == 0 {
			return fmt.Errorf("usage: omapad mode <auto|manual|standby>")
		}
		mode, err := walkingpad.ParseMode(arguments[0])
		if err != nil {
			return err
		}
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandSetMode, Mode: &mode}, hasFlag(arguments, "--json"))
	case "scan":
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandStartScanning}, hasFlag(arguments, "--json"))
	case "connect":
		request := walkingpad.CommandRequest{Command: walkingpad.CommandConnect}
		if len(arguments) > 0 {
			request.PeripheralID = arguments[0]
		}
		return sendAndPrint(ctx, client, request, hasFlag(arguments, "--json"))
	case "disconnect":
		return sendAndPrint(ctx, client, walkingpad.CommandRequest{Command: walkingpad.CommandDisconnect}, hasFlag(arguments, "--json"))
	case "doctor":
		return doctor(ctx, client, socketPath)
	default:
		return fmt.Errorf("unknown command %q (try omapad help)", command)
	}
}

func runDaemon(arguments []string, socketPath string) error {
	device := ""
	for len(arguments) > 0 {
		switch arguments[0] {
		case "--socket":
			if len(arguments) < 2 {
				return fmt.Errorf("--socket requires a path")
			}
			socketPath, arguments = arguments[1], arguments[2:]
		case "--device":
			if len(arguments) < 2 {
				return fmt.Errorf("--device requires a name or Bluetooth address")
			}
			device, arguments = arguments[1], arguments[2:]
		default:
			return fmt.Errorf("unknown daemon option %q", arguments[0])
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return daemon.New(device).Run(ctx, socketPath)
}

func sendAndPrint(ctx context.Context, client *ipc.Client, request walkingpad.CommandRequest, jsonOutput bool) error {
	response, err := client.Command(ctx, request)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(response)
	}
	printStatus(response.Snapshot)
	return nil
}

func watch(client *ipc.Client, jsonOutput bool) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		snapshot, err := client.State(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "omapad:", err)
		} else if err == nil {
			if jsonOutput {
				data, _ := json.Marshal(snapshot)
				fmt.Println(string(data))
			} else {
				printStatus(snapshot)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func doctor(ctx context.Context, client *ipc.Client, socketPath string) error {
	fmt.Println("OmaPad diagnostics")
	fmt.Println("  Socket:", socketPath)
	snapshot, err := client.State(ctx)
	if err != nil {
		fmt.Println("  Service: unavailable")
		return err
	}
	fmt.Println("  Service: reachable")
	fmt.Println("  Bluetooth:", snapshot.ConnectionState)
	if len(snapshot.DiscoveredDevices) > 0 {
		fmt.Printf("  Device: %s\n", snapshot.DiscoveredDevices[0].Name)
	}
	return nil
}

func printStatus(snapshot walkingpad.Snapshot) {
	runState := "stopped"
	if snapshot.IsRunning {
		runState = "walking"
	}
	fmt.Printf("%-12s %4.1f km/h  %s  %.2f km  %d steps  [%s]\n", runState, snapshot.Status.Speed, snapshot.DurationText(), snapshot.SessionDistance, snapshot.SessionSteps, snapshot.ConnectionState)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printHelp() {
	fmt.Print(`OmaPad controls WalkingPad treadmills from Linux.

Usage:
  omapad [--socket PATH]                         Open the dashboard
  omapad [--socket PATH] status [--json]
  omapad [--socket PATH] watch [--json]
  omapad [--socket PATH] start|pause|stop
  omapad [--socket PATH] speed <0.5-6.0>
  omapad [--socket PATH] mode <auto|manual|standby>
  omapad [--socket PATH] scan|connect [device]|disconnect
  omapad daemon [--socket PATH] [--device name-or-address]
  omapad doctor

Environment:
  OMAPAD_SOCKET   Local Unix socket (default $XDG_RUNTIME_DIR/omapad.sock)

Dashboard keys:
  h/l or arrows   Adjust target speed by 0.5 km/h
  space           Start or pause
  s               Stop and reset the session
  r               Refresh
  q               Quit
`)
}

func hasFlag(arguments []string, flag string) bool {
	for _, argument := range arguments {
		if argument == flag {
			return true
		}
	}
	return false
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
