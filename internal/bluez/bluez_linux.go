//go:build linux

package bluez

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Aayush9029/OmaPad/internal/walkingpad"
	"github.com/godbus/dbus/v5"
)

const (
	bluezService   = "org.bluez"
	objectManager  = "org.freedesktop.DBus.ObjectManager"
	properties     = "org.freedesktop.DBus.Properties"
	adapterIface   = "org.bluez.Adapter1"
	deviceIface    = "org.bluez.Device1"
	characterIface = "org.bluez.GattCharacteristic1"
)

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

type Device struct {
	conn          *dbus.Conn
	path          dbus.ObjectPath
	rxPath        dbus.ObjectPath
	txPath        dbus.ObjectPath
	signalChannel chan *dbus.Signal
	statuses      chan walkingpad.Status
	errors        chan error
	done          chan struct{}
	closeOnce     sync.Once
	writeMu       sync.Mutex
	lastWrite     time.Time

	Name    string
	Address string
}

func Connect(ctx context.Context, hint string) (*Device, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to system D-Bus: %w", err)
	}

	adapter, err := findAdapter(conn)
	if err != nil {
		return nil, err
	}
	path, propertiesMap, err := findDevice(conn, hint)
	if err != nil {
		filter := map[string]dbus.Variant{
			"Transport": dbus.MakeVariant("le"),
			"Pattern":   dbus.MakeVariant("WalkingPad"),
		}
		adapterObject := conn.Object(bluezService, adapter)
		if call := adapterObject.CallWithContext(ctx, adapterIface+".SetDiscoveryFilter", 0, filter); call.Err != nil {
			return nil, fmt.Errorf("set Bluetooth discovery filter: %w", call.Err)
		}
		if call := adapterObject.CallWithContext(ctx, adapterIface+".StartDiscovery", 0); call.Err != nil && !isBlueZError(call.Err, "InProgress") {
			return nil, fmt.Errorf("start Bluetooth discovery: %w", call.Err)
		}
		defer adapterObject.Call(adapterIface+".StopDiscovery", 0) //nolint:errcheck

		deadline := time.NewTimer(25 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(350 * time.Millisecond)
		defer ticker.Stop()
		for {
			path, propertiesMap, err = findDevice(conn, hint)
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-deadline.C:
				return nil, fmt.Errorf("WalkingPad not found after 25 seconds")
			case <-ticker.C:
			}
		}
	}

	deviceObject := conn.Object(bluezService, path)
	if call := deviceObject.CallWithContext(ctx, deviceIface+".Connect", 0); call.Err != nil && !isBlueZError(call.Err, "AlreadyConnected") {
		return nil, fmt.Errorf("connect to WalkingPad: %w", call.Err)
	}
	if err := waitForProperty(ctx, conn, path, deviceIface, "ServicesResolved", true, 15*time.Second); err != nil {
		return nil, fmt.Errorf("wait for WalkingPad services: %w", err)
	}

	rxPath, txPath, err := findCharacteristics(conn, path)
	if err != nil {
		return nil, err
	}
	name := variantString(propertiesMap["Name"])
	if name == "" {
		name = variantString(propertiesMap["Alias"])
	}
	device := &Device{
		conn:          conn,
		path:          path,
		rxPath:        rxPath,
		txPath:        txPath,
		signalChannel: make(chan *dbus.Signal, 32),
		statuses:      make(chan walkingpad.Status, 16),
		errors:        make(chan error, 4),
		done:          make(chan struct{}),
		Name:          name,
		Address:       variantString(propertiesMap["Address"]),
	}

	match := []dbus.MatchOption{
		dbus.WithMatchInterface(properties),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath(rxPath),
	}
	if err := conn.AddMatchSignal(match...); err != nil {
		return nil, fmt.Errorf("subscribe to WalkingPad notifications: %w", err)
	}
	conn.Signal(device.signalChannel)
	if call := conn.Object(bluezService, rxPath).CallWithContext(ctx, characterIface+".StartNotify", 0); call.Err != nil && !isBlueZError(call.Err, "InProgress") {
		conn.RemoveSignal(device.signalChannel)
		_ = conn.RemoveMatchSignal(match...)
		return nil, fmt.Errorf("enable WalkingPad notifications: %w", call.Err)
	}
	go device.consumeSignals(match)
	return device, nil
}

func (d *Device) Statuses() <-chan walkingpad.Status { return d.statuses }
func (d *Device) Errors() <-chan error               { return d.errors }

func (d *Device) Write(ctx context.Context, command []byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if wait := 700*time.Millisecond - time.Since(d.lastWrite); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	options := map[string]dbus.Variant{"type": dbus.MakeVariant("command")}
	call := d.conn.Object(bluezService, d.txPath).CallWithContext(ctx, characterIface+".WriteValue", 0, command, options)
	if call.Err != nil {
		return fmt.Errorf("write WalkingPad command: %w", call.Err)
	}
	d.lastWrite = time.Now()
	return nil
}

func (d *Device) Close() error {
	var result error
	d.closeOnce.Do(func() {
		close(d.done)
		d.conn.Object(bluezService, d.rxPath).Call(characterIface+".StopNotify", 0) //nolint:errcheck
		d.conn.RemoveSignal(d.signalChannel)
		result = d.conn.Object(bluezService, d.path).Call(deviceIface+".Disconnect", 0).Err
	})
	return result
}

func (d *Device) consumeSignals(match []dbus.MatchOption) {
	defer close(d.statuses)
	defer close(d.errors)
	defer d.conn.RemoveMatchSignal(match...) //nolint:errcheck
	for {
		select {
		case <-d.done:
			return
		case signal := <-d.signalChannel:
			if signal == nil {
				return
			}
			if signal.Path != d.rxPath || signal.Name != properties+".PropertiesChanged" || len(signal.Body) < 2 {
				continue
			}
			iface, _ := signal.Body[0].(string)
			changed, _ := signal.Body[1].(map[string]dbus.Variant)
			if iface != characterIface {
				continue
			}
			value, ok := changed["Value"]
			if !ok {
				continue
			}
			data, ok := value.Value().([]byte)
			if !ok {
				continue
			}
			status, err := walkingpad.ParseStatus(data)
			if err != nil {
				select {
				case d.errors <- err:
				default:
				}
				continue
			}
			select {
			case d.statuses <- status:
			case <-d.done:
				return
			}
		}
	}
}

func findAdapter(conn *dbus.Conn) (dbus.ObjectPath, error) {
	objects, err := getManagedObjects(conn)
	if err != nil {
		return "", err
	}
	for path, interfaces := range objects {
		if _, ok := interfaces[adapterIface]; ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("no BlueZ adapter found")
}

func findDevice(conn *dbus.Conn, hint string) (dbus.ObjectPath, map[string]dbus.Variant, error) {
	objects, err := getManagedObjects(conn)
	if err != nil {
		return "", nil, err
	}
	hint = strings.ToLower(strings.TrimSpace(hint))
	for path, interfaces := range objects {
		props, ok := interfaces[deviceIface]
		if !ok {
			continue
		}
		name := strings.ToLower(variantString(props["Name"]))
		alias := strings.ToLower(variantString(props["Alias"]))
		address := strings.ToLower(variantString(props["Address"]))
		if hint != "" {
			if hint == address || strings.Contains(name, hint) || strings.Contains(alias, hint) {
				return path, props, nil
			}
			continue
		}
		if strings.Contains(name, "walkingpad") || strings.Contains(alias, "walkingpad") || hasUUID(props["UUIDs"], walkingpad.ServiceUUID) {
			return path, props, nil
		}
	}
	return "", nil, fmt.Errorf("WalkingPad not found")
}

func findCharacteristics(conn *dbus.Conn, devicePath dbus.ObjectPath) (dbus.ObjectPath, dbus.ObjectPath, error) {
	objects, err := getManagedObjects(conn)
	if err != nil {
		return "", "", err
	}
	var rx, tx dbus.ObjectPath
	for path, interfaces := range objects {
		props, ok := interfaces[characterIface]
		if !ok || !strings.HasPrefix(string(path), string(devicePath)+"/") {
			continue
		}
		uuid := variantString(props["UUID"])
		switch {
		case uuidEqual(uuid, walkingpad.RXUUID):
			rx = path
		case uuidEqual(uuid, walkingpad.TXUUID):
			tx = path
		}
	}
	if rx == "" || tx == "" {
		return "", "", fmt.Errorf("WalkingPad FE01/FE02 characteristics were not found")
	}
	return rx, tx, nil
}

func getManagedObjects(conn *dbus.Conn) (managedObjects, error) {
	var objects managedObjects
	call := conn.Object(bluezService, "/").Call(objectManager+".GetManagedObjects", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("read BlueZ objects: %w", call.Err)
	}
	if err := call.Store(&objects); err != nil {
		return nil, fmt.Errorf("decode BlueZ objects: %w", err)
	}
	return objects, nil
}

func waitForProperty(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, iface, name string, want bool, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		property, err := conn.Object(bluezService, path).GetProperty(iface + "." + name)
		if err == nil {
			if value, ok := property.Value().(bool); ok && value == want {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s", name)
		case <-ticker.C:
		}
	}
}

func variantString(value dbus.Variant) string {
	if text, ok := value.Value().(string); ok {
		return text
	}
	return ""
}

func hasUUID(value dbus.Variant, want string) bool {
	uuids, _ := value.Value().([]string)
	for _, uuid := range uuids {
		if uuidEqual(uuid, want) {
			return true
		}
	}
	return false
}

func uuidEqual(left, right string) bool {
	return strings.EqualFold(strings.ReplaceAll(left, "-", ""), strings.ReplaceAll(right, "-", ""))
}

func isBlueZError(err error, suffix string) bool {
	return strings.Contains(err.Error(), "org.bluez.Error."+suffix)
}
