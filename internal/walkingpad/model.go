package walkingpad

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ConnectionState string

const (
	Disconnected ConnectionState = "disconnected"
	Scanning     ConnectionState = "scanning"
	Connecting   ConnectionState = "connecting"
	Connected    ConnectionState = "connected"
	Ready        ConnectionState = "ready"
)

// UnmarshalJSON accepts both the normalized Linux representation ("ready")
// and Swift Codable's synthesized enum representation ({"ready":{}}).
func (s *ConnectionState) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*s = ConnectionState(strings.ToLower(value))
		return nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || len(object) != 1 {
		return fmt.Errorf("invalid connection state: %s", bytes.TrimSpace(data))
	}
	for key := range object {
		*s = ConnectionState(strings.ToLower(key))
	}
	return nil
}

type Mode uint8

const (
	ModeAuto Mode = iota
	ModeManual
	ModeStandby
)

func (m Mode) String() string {
	switch m {
	case ModeAuto:
		return "auto"
	case ModeManual:
		return "manual"
	case ModeStandby:
		return "standby"
	default:
		return "unknown"
	}
}

func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(value) {
	case "auto":
		return ModeAuto, nil
	case "manual":
		return ModeManual, nil
	case "standby":
		return ModeStandby, nil
	default:
		return 0, fmt.Errorf("mode must be auto, manual, or standby")
	}
}

type Status struct {
	Speed      float64 `json:"speed"`
	Mode       Mode    `json:"mode"`
	Time       float64 `json:"time"`
	DistanceKM float64 `json:"distanceKM"`
	Steps      int     `json:"steps"`
}

type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
}

type Snapshot struct {
	ConnectionState   ConnectionState `json:"connectionState"`
	Status            Status          `json:"status"`
	DiscoveredDevices []Device        `json:"discoveredDevices"`
	IsRunning         bool            `json:"isRunning"`
	TargetSpeed       float64         `json:"targetSpeed"`
	Error             *string         `json:"error,omitempty"`
	SessionTime       float64         `json:"sessionTime"`
	SessionDistance   float64         `json:"sessionDistance"`
	SessionSteps      int             `json:"sessionSteps"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

func (s Snapshot) Duration() time.Duration {
	return time.Duration(s.SessionTime * float64(time.Second))
}

func (s Snapshot) DurationText() string {
	total := int(s.SessionTime)
	hours := total / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

type CommandKind string

const (
	CommandStartScanning CommandKind = "startScanning"
	CommandStopScanning  CommandKind = "stopScanning"
	CommandConnect       CommandKind = "connect"
	CommandDisconnect    CommandKind = "disconnect"
	CommandStartPause    CommandKind = "startPause"
	CommandStop          CommandKind = "stop"
	CommandSetSpeed      CommandKind = "setSpeed"
	CommandSetMode       CommandKind = "setMode"
)

type CommandRequest struct {
	Command      CommandKind `json:"command"`
	PeripheralID string      `json:"peripheralID,omitempty"`
	Speed        *float64    `json:"speed,omitempty"`
	Mode         *Mode       `json:"mode,omitempty"`
}

type CommandResponse struct {
	OK       bool     `json:"ok"`
	Message  *string  `json:"message,omitempty"`
	Snapshot Snapshot `json:"snapshot"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
