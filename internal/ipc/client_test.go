package ipc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/Aayush9029/OmaPad/internal/walkingpad"
)

func TestClientReadsSnapshotOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "omapad.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"connectionState":"ready","status":{"speed":2,"mode":1,"time":258,"distanceKM":0.14,"steps":507},"discoveredDevices":[],"isRunning":true,"targetSpeed":2,"sessionTime":3,"sessionDistance":0.01,"sessionSteps":7,"updatedAt":"2026-08-24T19:31:06Z"}`)
	})}
	go server.Serve(listener) //nolint:errcheck
	t.Cleanup(func() { _ = server.Close() })

	client, err := New(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ConnectionState != walkingpad.Ready || snapshot.Status.Speed != 2 || snapshot.SessionSteps != 7 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}
