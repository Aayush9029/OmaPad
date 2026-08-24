package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Aayush9029/OmaPad/internal/walkingpad"
)

const socketName = "omapad.sock"

type Client struct {
	socketPath string
	http       *http.Client
}

func DefaultSocketPath() string {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, socketName)
	}
	return filepath.Join(os.TempDir(), "omapad-"+strconv.Itoa(os.Getuid())+".sock")
}

func New(socketPath string) (*Client, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("WalkingPad socket path is empty")
	}
	if !filepath.IsAbs(socketPath) {
		return nil, fmt.Errorf("WalkingPad socket path must be absolute: %s", socketPath)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socketPath: socketPath,
		http:       &http.Client{Transport: transport, Timeout: 5 * time.Second},
	}, nil
}

func (c *Client) State(ctx context.Context) (walkingpad.Snapshot, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://omapad/state", nil)
	if err != nil {
		return walkingpad.Snapshot{}, err
	}
	var snapshot walkingpad.Snapshot
	if err := c.do(request, &snapshot); err != nil {
		return walkingpad.Snapshot{}, err
	}
	return snapshot, nil
}

func (c *Client) Command(ctx context.Context, command walkingpad.CommandRequest) (walkingpad.CommandResponse, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return walkingpad.CommandResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://omapad/command", bytes.NewReader(body))
	if err != nil {
		return walkingpad.CommandResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	var response walkingpad.CommandResponse
	if err := c.do(request, &response); err != nil {
		return response, err
	}
	if !response.OK {
		if response.Message != nil {
			return response, fmt.Errorf("%s", *response.Message)
		}
		return response, fmt.Errorf("WalkingPad command failed")
	}
	return response, nil
}

func (c *Client) do(request *http.Request, target any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("WalkingPad service is unavailable at %s: %w", c.socketPath, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var serverError walkingpad.ErrorResponse
		if json.Unmarshal(body, &serverError) == nil && serverError.Error != "" {
			return fmt.Errorf("%s", serverError.Error)
		}
		var commandResponse walkingpad.CommandResponse
		if json.Unmarshal(body, &commandResponse) == nil && commandResponse.Message != nil {
			return fmt.Errorf("%s", *commandResponse.Message)
		}
		return fmt.Errorf("WalkingPad service returned status %d", response.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("invalid WalkingPad response: %w", err)
	}
	return nil
}
