package alerts

import (
	"testing"
	"time"

	"golang.org/x/net/context"
)

func TestPollingAlert(t *testing.T) {
	var isCalled bool
	tg := NewTriggerOptions(
		WithName("TestPollingAlert"),
		WithMinCount(2),
		WithThresholdRate(0.6),
		WithWindowTime(2*time.Millisecond),
		WithCallback(func(ctx context.Context) {
			isCalled = true
		}),
		WithPollingInterval(time.Millisecond),
	)

	al := NewAlert(WithTrigger(tg))
	al.IncrSuccess()
	al.IncrError()
	if isCalled {
		t.Error("isCalled == true")
	}
	al.IncrError()
	if !isCalled {
		t.Error("isCalled != true")
	}
	isCalled = false
	// wait for polling
	time.Sleep(2 * time.Millisecond)
	if !isCalled {
		t.Error("isCalled != true")
	}
}

func TestNoPollingAlert(t *testing.T) {
	var isCalled bool
	tg := NewTriggerOptions(
		WithName("TestPollingAlert"),
		WithMinCount(2),
		WithThresholdRate(0.6),
		WithWindowTime(time.Millisecond),
		WithCallback(func(ctx context.Context) {
			isCalled = true
		}),
		WithNoPolling(true),
	)

	al := NewAlert(WithTrigger(tg))
	al.IncrSuccess()
	al.IncrError()
	if isCalled {
		t.Error("isCalled == true")
	}
	al.IncrError()
	if !isCalled {
		t.Error("isCalled != true")
	}

	// go next window
	time.Sleep(time.Millisecond)
	isCalled = false

	al.IncrError()
	if isCalled {
		t.Error("isCalled == true")
	}
	al.IncrError()
	if !isCalled {
		t.Error("isCalled != true")
	}
}
