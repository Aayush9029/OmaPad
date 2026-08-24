package daemon

import (
	"testing"

	"github.com/Aayush9029/OmaPad/internal/walkingpad"
)

func TestUpdateStatusUsesDeviceStepDeltas(t *testing.T) {
	manager := New("")
	manager.updateStatus(walkingpad.Status{Speed: 2, Time: 100, DistanceKM: 1, Steps: 500})
	manager.updateStatus(walkingpad.Status{Speed: 2, Time: 101, DistanceKM: 1, Steps: 502})

	snapshot := manager.state()
	if snapshot.SessionSteps != 2 {
		t.Fatalf("got %d session steps, want 2", snapshot.SessionSteps)
	}
	if snapshot.SessionTime != 1 {
		t.Fatalf("got %.0f session seconds, want 1", snapshot.SessionTime)
	}
}

func TestUpdateStatusIgnoresCounterReset(t *testing.T) {
	manager := New("")
	manager.updateStatus(walkingpad.Status{Speed: 2, Time: 100, DistanceKM: 1, Steps: 500})
	manager.updateStatus(walkingpad.Status{Speed: 2, Time: 1, DistanceKM: 0, Steps: 3})

	if got := manager.state().SessionSteps; got != 0 {
		t.Fatalf("got %d session steps after counter reset, want 0", got)
	}
}
