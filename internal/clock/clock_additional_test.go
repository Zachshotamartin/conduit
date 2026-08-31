package clock_test

import (
	"reflect"
	"testing"
	"time"

	conduitclock "github.com/Zachshotamartin/conduit/internal/clock"
)

func TestFakeRejectsNegativeAdvanceWithoutChangingState(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	fake := conduitclock.NewFake(start)
	fired := false
	fake.Schedule(0, func(time.Time) {
		fired = true
	})

	func() {
		defer func() {
			if got := recover(); got != conduitclock.ErrNegativeAdvance {
				t.Fatalf("Advance(-1ns) panic = %#v, want ErrNegativeAdvance", got)
			}
		}()
		fake.Advance(-time.Nanosecond)
	}()

	if got := fake.Now(); !got.Equal(start) {
		t.Fatalf("Now() after rejected advance = %s, want unchanged %s", got, start)
	}
	if fired {
		t.Fatal("timer fired during rejected negative advance")
	}

	fake.Advance(0)
	if !fired {
		t.Fatal("zero-duration timer did not fire on Advance(0)")
	}
}

func TestFakeDrainsTimersScheduledByCallbackThroughSameTarget(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0).UTC()
	fake := conduitclock.NewFake(start)
	type firing struct {
		name string
		at   time.Time
	}
	var got []firing
	fake.Schedule(time.Second, func(now time.Time) {
		got = append(got, firing{name: "outer", at: now})
		fake.Schedule(0, func(innerNow time.Time) {
			got = append(got, firing{name: "inner", at: innerNow})
		})
		cancelled := fake.Schedule(0, func(cancelledNow time.Time) {
			got = append(got, firing{name: "cancelled", at: cancelledNow})
		})
		if !fake.Cancel(cancelled) {
			t.Error("Cancel(timer scheduled by callback) = false, want true")
		}
	})

	fake.Advance(time.Second)
	if want := []firing{
		{name: "outer", at: start.Add(time.Second)},
		{name: "inner", at: start.Add(time.Second)},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callback sequence = %#v, want %#v", got, want)
	}
}

func TestRealClockSupportsInjectionAndExactCancellation(t *testing.T) {
	t.Parallel()

	realClock := conduitclock.NewReal()
	var injected conduitclock.Clock = realClock
	if got := injected.Now(); got.IsZero() {
		t.Fatal("Real.Now() returned the zero time")
	}

	handle := injected.Schedule(time.Hour, func(time.Time) {
		t.Error("cancelled real-clock callback fired")
	})
	if !injected.Cancel(handle) {
		t.Fatal("Cancel(pending real-clock handle) = false, want true")
	}
	if injected.Cancel(handle) {
		t.Fatal("Cancel(cancelled real-clock handle) = true, want false")
	}
	if injected.Cancel(conduitclock.TimerHandle{}) {
		t.Fatal("Cancel(zero handle) = true, want false")
	}

	other := conduitclock.NewReal()
	otherHandle := other.Schedule(time.Hour, func(time.Time) {
		t.Error("cancelled foreign real-clock callback fired")
	})
	if injected.Cancel(otherHandle) {
		t.Fatal("Cancel(foreign real-clock handle) = true, want false")
	}
	if !other.Cancel(otherHandle) {
		t.Fatal("issuing real clock could not cancel its own handle")
	}
}

func TestRealZeroValueIsReadyForUse(t *testing.T) {
	t.Parallel()

	var realClock conduitclock.Real
	handle := realClock.Schedule(time.Hour, func(time.Time) {
		t.Error("cancelled zero-value Real callback fired")
	})
	if !realClock.Cancel(handle) {
		t.Fatal("zero-value Real could not cancel its pending timer")
	}
}

func TestScheduleRejectsNilCallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schedule func()
	}{
		{
			name: "fake",
			schedule: func() {
				conduitclock.NewFake(time.Unix(0, 0).UTC()).Schedule(time.Second, nil)
			},
		},
		{
			name: "real",
			schedule: func() {
				conduitclock.NewReal().Schedule(time.Second, nil)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if got := recover(); got != "clock: nil callback" {
					t.Fatalf("Schedule(nil) panic = %#v, want %q", got, "clock: nil callback")
				}
			}()
			test.schedule()
		})
	}
}
