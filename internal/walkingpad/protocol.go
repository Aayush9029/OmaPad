package walkingpad

import "fmt"

const (
	ServiceUUID = "0000fe00-0000-1000-8000-00805f9b34fb"
	RXUUID      = "0000fe01-0000-1000-8000-00805f9b34fb"
	TXUUID      = "0000fe02-0000-1000-8000-00805f9b34fb"
)

func ChangeSpeed(speed float64) ([]byte, error) {
	if speed < 0 || speed > 6 {
		return nil, fmt.Errorf("speed must be between 0 and 6 km/h")
	}
	return buildCommand(0x01, byte(speed*10)), nil
}

func ChangeMode(mode Mode) []byte { return buildCommand(0x02, byte(mode)) }
func StartBelt() []byte           { return buildCommand(0x04, 0x01) }
func StopBelt() []byte            { data, _ := ChangeSpeed(0); return data }
func AskStats() []byte            { return []byte{0xF7, 0xA2, 0x00, 0x00, 0xA2, 0xFD} }

func buildCommand(command, parameter byte) []byte {
	data := []byte{0xF7, 0xA2, command, parameter, 0x00, 0xFD}
	data[4] = data[1] + data[2] + data[3]
	return data
}

func ParseStatus(data []byte) (Status, error) {
	if len(data) < 14 || data[0] != 0xF8 || data[1] != 0xA2 {
		return Status{}, fmt.Errorf("invalid WalkingPad status packet")
	}
	payload := data[2:]
	seconds := int(payload[3])<<16 | int(payload[4])<<8 | int(payload[5])
	distance := int(payload[6])<<16 | int(payload[7])<<8 | int(payload[8])
	steps := int(payload[9])<<16 | int(payload[10])<<8 | int(payload[11])
	mode := Mode(payload[2])
	if mode > ModeStandby {
		mode = ModeStandby
	}
	return Status{
		Speed:      float64(payload[1]) / 10,
		Mode:       mode,
		Time:       float64(seconds),
		DistanceKM: float64(distance) / 100,
		Steps:      steps,
	}, nil
}
