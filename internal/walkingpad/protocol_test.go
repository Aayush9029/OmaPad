package walkingpad

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCommandsMatchSwiftImplementation(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"start", StartBelt(), []byte{0xF7, 0xA2, 0x04, 0x01, 0xA7, 0xFD}},
		{"stop", StopBelt(), []byte{0xF7, 0xA2, 0x01, 0x00, 0xA3, 0xFD}},
		{"stats", AskStats(), []byte{0xF7, 0xA2, 0x00, 0x00, 0xA2, 0xFD}},
		{"manual", ChangeMode(ModeManual), []byte{0xF7, 0xA2, 0x02, 0x01, 0xA5, 0xFD}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.got, test.want) {
				t.Fatalf("got % X, want % X", test.got, test.want)
			}
		})
	}

	speed, err := ChangeSpeed(2.5)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0xF7, 0xA2, 0x01, 0x19, 0xBC, 0xFD}; !reflect.DeepEqual(speed, want) {
		t.Fatalf("got % X, want % X", speed, want)
	}
}

func TestParseStatus(t *testing.T) {
	status, err := ParseStatus([]byte{
		0xF8, 0xA2, 0x00, 0x14, 0x01,
		0x00, 0x01, 0x02,
		0x00, 0x00, 0x0E,
		0x00, 0x01, 0xFB,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Speed != 2 || status.Mode != ModeManual || status.Time != 258 || status.DistanceKM != 0.14 || status.Steps != 507 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestConnectionStateDecodesSwiftAndNormalizedJSON(t *testing.T) {
	for _, input := range []string{`"ready"`, `{"ready":{}}`} {
		var state ConnectionState
		if err := json.Unmarshal([]byte(input), &state); err != nil {
			t.Fatal(err)
		}
		if state != Ready {
			t.Fatalf("got %q", state)
		}
	}
}
