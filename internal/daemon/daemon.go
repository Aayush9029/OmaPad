package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Aayush9029/OmaPad/internal/bluez"
	"github.com/Aayush9029/OmaPad/internal/walkingpad"
)

type Manager struct {
	mu         sync.RWMutex
	snapshot   walkingpad.Snapshot
	device     *bluez.Device
	enabled    bool
	deviceHint string
	wake       chan struct{}
}

func New(deviceHint string) *Manager {
	return &Manager{
		snapshot: walkingpad.Snapshot{
			ConnectionState:   walkingpad.Disconnected,
			Status:            walkingpad.Status{Mode: walkingpad.ModeStandby},
			DiscoveredDevices: []walkingpad.Device{},
			TargetSpeed:       2.5,
			UpdatedAt:         time.Now(),
		},
		enabled:    true,
		deviceHint: deviceHint,
		wake:       make(chan struct{}, 1),
	}
}

func (m *Manager) Run(ctx context.Context, socketPath string) error {
	if err := prepareSocket(socketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on WalkingPad socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath) //nolint:errcheck
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("secure WalkingPad socket: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", m.handleState)
	mux.HandleFunc("POST /command", m.handleCommand)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go m.connectionLoop(ctx)
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	log.Printf("WalkingPad service listening on unix://%s", socketPath)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (m *Manager) connectionLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			m.closeDevice()
			return
		}
		m.mu.RLock()
		enabled := m.enabled
		m.mu.RUnlock()
		if !enabled {
			select {
			case <-ctx.Done():
				continue
			case <-m.wake:
				continue
			}
		}

		m.setConnection(walkingpad.Scanning, nil)
		device, err := bluez.Connect(ctx, m.deviceHint)
		if err != nil {
			m.setConnection(walkingpad.Disconnected, err)
			if !wait(ctx, m.wake, 5*time.Second) {
				continue
			}
			continue
		}

		m.mu.Lock()
		m.device = device
		m.snapshot.ConnectionState = walkingpad.Ready
		m.snapshot.DiscoveredDevices = []walkingpad.Device{{ID: device.Address, Name: device.Name, Address: device.Address}}
		m.snapshot.Error = nil
		m.snapshot.UpdatedAt = time.Now()
		m.mu.Unlock()
		if err := device.Write(ctx, walkingpad.AskStats()); err != nil {
			m.setError(err)
		}

		poll := time.NewTicker(time.Second)
		connected := true
		for connected {
			select {
			case <-ctx.Done():
				connected = false
			case status, ok := <-device.Statuses():
				if !ok {
					connected = false
					break
				}
				m.updateStatus(status)
			case err, ok := <-device.Errors():
				if ok {
					m.setError(err)
				}
			case <-poll.C:
				if err := device.Write(ctx, walkingpad.AskStats()); err != nil {
					m.setError(err)
					connected = false
				}
			case <-m.wake:
				m.mu.RLock()
				stillEnabled := m.enabled
				m.mu.RUnlock()
				if !stillEnabled {
					connected = false
				}
			}
		}
		poll.Stop()
		m.closeDevice()
		m.mu.RLock()
		reconnect := m.enabled
		m.mu.RUnlock()
		if reconnect && ctx.Err() == nil {
			m.setConnection(walkingpad.Disconnected, fmt.Errorf("WalkingPad connection was lost; reconnecting"))
		}
	}
}

func (m *Manager) updateStatus(status walkingpad.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.snapshot.Status
	wasRunning := m.snapshot.IsRunning
	isRunning := status.Speed > 0
	if wasRunning && isRunning {
		seconds := status.Time - previous.Time
		distance := status.DistanceKM - previous.DistanceKM
		steps := status.Steps - previous.Steps
		if seconds >= 0 {
			m.snapshot.SessionTime += seconds
		}
		if distance >= 0 {
			m.snapshot.SessionDistance += distance
		}
		if steps >= 0 {
			m.snapshot.SessionSteps += steps
		}
	}
	m.snapshot.Status = status
	m.snapshot.IsRunning = isRunning
	if isRunning && m.snapshot.TargetSpeed == 0 {
		m.snapshot.TargetSpeed = status.Speed
	}
	m.snapshot.ConnectionState = walkingpad.Ready
	m.snapshot.Error = nil
	m.snapshot.UpdatedAt = time.Now()
}

func (m *Manager) handleState(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, m.state())
}

func (m *Manager) handleCommand(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var command walkingpad.CommandRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 64<<10))
	if err := decoder.Decode(&command); err != nil {
		writeJSON(writer, http.StatusBadRequest, walkingpad.ErrorResponse{Error: "Invalid command: " + err.Error()})
		return
	}
	if err := m.command(request.Context(), command); err != nil {
		message := err.Error()
		m.setError(err)
		writeJSON(writer, http.StatusBadRequest, walkingpad.CommandResponse{OK: false, Message: &message, Snapshot: m.state()})
		return
	}
	writeJSON(writer, http.StatusOK, walkingpad.CommandResponse{OK: true, Snapshot: m.state()})
}

func (m *Manager) command(ctx context.Context, command walkingpad.CommandRequest) error {
	switch command.Command {
	case walkingpad.CommandStartScanning, walkingpad.CommandConnect:
		m.mu.Lock()
		m.enabled = true
		m.snapshot.ConnectionState = walkingpad.Scanning
		m.snapshot.Error = nil
		m.snapshot.UpdatedAt = time.Now()
		m.mu.Unlock()
		m.notify()
		return nil
	case walkingpad.CommandStopScanning, walkingpad.CommandDisconnect:
		m.mu.Lock()
		m.enabled = false
		m.snapshot.ConnectionState = walkingpad.Disconnected
		m.snapshot.UpdatedAt = time.Now()
		m.mu.Unlock()
		m.notify()
		return nil
	case walkingpad.CommandStartPause:
		state := m.state()
		if state.IsRunning {
			return m.write(ctx, walkingpad.StopBelt())
		}
		if state.Status.Mode == walkingpad.ModeStandby {
			if err := m.write(ctx, walkingpad.ChangeMode(walkingpad.ModeManual)); err != nil {
				return err
			}
		}
		if err := m.write(ctx, walkingpad.StartBelt()); err != nil {
			return err
		}
		if err := sleepContext(ctx, 2*time.Second); err != nil {
			return err
		}
		speed := state.TargetSpeed
		if speed < 0.5 {
			speed = 2.5
		}
		data, _ := walkingpad.ChangeSpeed(speed)
		return m.write(ctx, data)
	case walkingpad.CommandStop:
		if err := m.write(ctx, walkingpad.StopBelt()); err != nil {
			return err
		}
		m.mu.Lock()
		m.snapshot.IsRunning = false
		m.snapshot.SessionTime = 0
		m.snapshot.SessionDistance = 0
		m.snapshot.SessionSteps = 0
		m.snapshot.UpdatedAt = time.Now()
		m.mu.Unlock()
		return nil
	case walkingpad.CommandSetSpeed:
		if command.Speed == nil || *command.Speed < 0.5 || *command.Speed > 6 {
			return fmt.Errorf("speed must be between 0.5 and 6.0 km/h")
		}
		m.mu.Lock()
		m.snapshot.TargetSpeed = *command.Speed
		running := m.snapshot.IsRunning
		m.snapshot.UpdatedAt = time.Now()
		m.mu.Unlock()
		if !running {
			return nil
		}
		data, _ := walkingpad.ChangeSpeed(*command.Speed)
		return m.write(ctx, data)
	case walkingpad.CommandSetMode:
		if command.Mode == nil {
			return fmt.Errorf("mode is required")
		}
		return m.write(ctx, walkingpad.ChangeMode(*command.Mode))
	default:
		return fmt.Errorf("unknown command %q", command.Command)
	}
}

func (m *Manager) write(ctx context.Context, data []byte) error {
	m.mu.RLock()
	device := m.device
	m.mu.RUnlock()
	if device == nil {
		return fmt.Errorf("WalkingPad is not connected")
	}
	return device.Write(ctx, data)
}

func (m *Manager) state() walkingpad.Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := m.snapshot
	result.DiscoveredDevices = append([]walkingpad.Device(nil), m.snapshot.DiscoveredDevices...)
	return result
}

func (m *Manager) closeDevice() {
	m.mu.Lock()
	device := m.device
	m.device = nil
	if m.snapshot.ConnectionState != walkingpad.Disconnected {
		m.snapshot.ConnectionState = walkingpad.Disconnected
		m.snapshot.UpdatedAt = time.Now()
	}
	m.mu.Unlock()
	if device != nil {
		_ = device.Close()
	}
}

func (m *Manager) setConnection(state walkingpad.ConnectionState, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.ConnectionState = state
	m.snapshot.UpdatedAt = time.Now()
	if err == nil {
		m.snapshot.Error = nil
	} else {
		message := err.Error()
		m.snapshot.Error = &message
	}
}

func (m *Manager) setError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	message := err.Error()
	m.snapshot.Error = &message
	m.snapshot.UpdatedAt = time.Now()
}

func (m *Manager) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func wait(ctx context.Context, wake <-chan struct{}, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	case <-wake:
		return true
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func prepareSocket(socketPath string) error {
	if !filepath.IsAbs(socketPath) {
		return fmt.Errorf("WalkingPad socket path must be absolute: %s", socketPath)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("create WalkingPad socket directory: %w", err)
	}
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect WalkingPad socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket path: %s", socketPath)
	}
	connection, dialErr := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("WalkingPad service is already running at %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale WalkingPad socket: %w", err)
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
