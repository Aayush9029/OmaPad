//go:build !linux

package bluez

import (
	"context"
	"fmt"

	"github.com/Aayush9029/OmaPad/internal/walkingpad"
)

type Device struct {
	Name    string
	Address string
}

func Connect(context.Context, string) (*Device, error) {
	return nil, fmt.Errorf("the native Bluetooth daemon currently requires Linux with BlueZ")
}

func (*Device) Statuses() <-chan walkingpad.Status  { return nil }
func (*Device) Errors() <-chan error                { return nil }
func (*Device) Write(context.Context, []byte) error { return fmt.Errorf("BlueZ is unavailable") }
func (*Device) Close() error                        { return nil }
