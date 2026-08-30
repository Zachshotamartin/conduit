package clock_test

import (
	"reflect"
	"testing"
	"time"

	conduitclock "github.com/Zachshotamartin/conduit/internal/clock"
)

var _ conduitclock.Clock = (*conduitclock.Fake)(nil)

func TestFakeClockImplementsInjectedClock(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 30, 12, 0, 0, 123, time.UTC)
	fake := conduitclock.NewFake(start)

	if got := readInjectedClock(fake); !got.Equal(start) {
		t.Fatalf("injected Clock.Now() = %s, want %s", got, start)
	}
}

func TestAdvanceMovesTimeOnlyWhenExplicitlyRequested(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	fake := conduitclock.NewFake(start)

	if got := fake.Now(); !got.Equal(start) {
		t.Fatalf("initial Now() = %s, want %s", got, start)
	}

	fake.Advance(250 * time.Millisecond)
	if got, want := fake.Now(), start.Add(250*time.Millisecond); !got.Equal(want) {
		t.Fatalf("Now() after first Advance = %s, want %s", got, want)
	}

	fake.Advance(750 * time.Millisecond)
	if got, want := fake.Now(), start.Add(time.Second); !got.Equal(want) {
		t.Fatalf("Now() after second Advance = %s, want %s", got, want)
	}
}

func TestScheduledCallbacksFireAtDeadlinesInDeterministicOrder(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	fake := conduitclock.NewFake(start)
	type firing struct {
		name string
		at   time.Time
	}
	var got []firing

	scheduleInjectedClock(fake, 3*time.Second, func() {
		got = append(got, firing{name: "third", at: fake.Now()})
	})
	scheduleInjectedClock(fake, time.Second, func() {
		got = append(got, firing{name: "first", at: fake.Now()})
	})
	scheduleInjectedClock(fake, 2*time.Second, func() {
		got = append(got, firing{name: "second", at: fake.Now()})
	})

	assertFirings(t, got, []firing(nil))
	fake.Advance(999 * time.Millisecond)
	assertFirings(t, got, []firing(nil))

	fake.Advance(1 * time.Millisecond)
	assertFirings(t, got, []firing{
		{name: "first", at: start.Add(time.Second)},
	})

	fake.Advance(2 * time.Second)
	assertFirings(t, got, []firing{
		{name: "first", at: start.Add(time.Second)},
		{name: "second", at: start.Add(2 * time.Second)},
		{name: "third", at: start.Add(3 * time.Second)},
	})
	if gotNow, wantNow := fake.Now(), start.Add(3*time.Second); !gotNow.Equal(wantNow) {
		t.Fatalf("Now() after firing callbacks = %s, want final advance target %s", gotNow, wantNow)
	}
}

func TestScheduledCallbacksUseStableInsertionOrderForEqualDeadlines(t *testing.T) {
	t.Parallel()

	fake := conduitclock.NewFake(time.Unix(0, 0).UTC())
	var got []int
	for i := 0; i < 8; i++ {
		i := i
		fake.Schedule(time.Second, func() {
			got = append(got, i)
		})
	}

	fake.Advance(time.Second)
	if want := []int{0, 1, 2, 3, 4, 5, 6, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("equal-deadline callback order = %v, want stable insertion order %v", got, want)
	}
}

func TestCancelPreventsScheduledCallbackAndReportsHandleState(t *testing.T) {
	t.Parallel()

	fake := conduitclock.NewFake(time.Unix(0, 0).UTC())
	fired := false
	handle := fake.Schedule(time.Second, func() {
		fired = true
	})

	if cancelled := cancelInjectedClock(fake, handle); !cancelled {
		t.Fatal("first Cancel(handle) = false, want true for a pending timer")
	}
	if cancelled := cancelInjectedClock(fake, handle); cancelled {
		t.Fatal("second Cancel(handle) = true, want false for an already-cancelled timer")
	}

	fake.Advance(time.Second)
	if fired {
		t.Fatal("cancelled callback fired")
	}
}

func TestTimerHandleCannotCancelAnotherTimer(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0).UTC()
	firstClock := conduitclock.NewFake(start)
	secondClock := conduitclock.NewFake(start)
	firstFired := false
	secondFired := false

	firstHandle := firstClock.Schedule(time.Second, func() {
		firstFired = true
	})
	secondHandle := secondClock.Schedule(time.Second, func() {
		secondFired = true
	})

	if cancelled := secondClock.Cancel(firstHandle); cancelled {
		t.Fatal("Cancel(handle from another clock) = true, want false")
	}
	if cancelled := firstClock.Cancel(secondHandle); cancelled {
		t.Fatal("Cancel(handle from another clock) = true, want false")
	}

	firstClock.Advance(time.Second)
	secondClock.Advance(time.Second)
	if !firstFired || !secondFired {
		t.Fatalf("cross-clock cancellation affected timers: first fired=%t, second fired=%t", firstFired, secondFired)
	}
}

func TestCancelAfterFireReportsTimerNoLongerPending(t *testing.T) {
	t.Parallel()

	fake := conduitclock.NewFake(time.Unix(0, 0).UTC())
	fireCount := 0
	handle := fake.Schedule(time.Second, func() {
		fireCount++
	})

	fake.Advance(time.Second)
	if fireCount != 1 {
		t.Fatalf("fire count = %d, want 1", fireCount)
	}
	if cancelled := fake.Cancel(handle); cancelled {
		t.Fatal("Cancel(fired handle) = true, want false")
	}
	fake.Advance(time.Hour)
	if fireCount != 1 {
		t.Fatalf("one-shot callback fire count = %d after later advance, want 1", fireCount)
	}
}

func TestAfterFiresOnlyOnAdvanceWithScheduledTimestamp(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	fake := conduitclock.NewFake(start)
	wake := afterInjectedClock(fake, 5*time.Second)

	assertNoTimeAvailable(t, wake)
	fake.Advance(5*time.Second - time.Nanosecond)
	assertNoTimeAvailable(t, wake)

	fake.Advance(time.Nanosecond)
	select {
	case got := <-wake:
		if want := start.Add(5 * time.Second); !got.Equal(want) {
			t.Fatalf("After timestamp = %s, want deadline %s", got, want)
		}
	default:
		t.Fatal("After channel did not fire synchronously when explicit Advance reached its deadline")
	}

	assertNoTimeAvailable(t, wake)
}

func readInjectedClock(c conduitclock.Clock) time.Time {
	return c.Now()
}

func afterInjectedClock(c conduitclock.Clock, d time.Duration) <-chan time.Time {
	return c.After(d)
}

func scheduleInjectedClock(c conduitclock.Clock, d time.Duration, fn func()) conduitclock.TimerHandle {
	return c.Schedule(d, fn)
}

func cancelInjectedClock(c conduitclock.Clock, h conduitclock.TimerHandle) bool {
	return c.Cancel(h)
}

func assertNoTimeAvailable(t *testing.T, ch <-chan time.Time) {
	t.Helper()

	select {
	case got := <-ch:
		t.Fatalf("unexpected timer event at %s before explicit advance reached the deadline", got)
	default:
	}
}

func assertFirings(t *testing.T, got, want any) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("firings = %#v, want %#v", got, want)
	}
}
