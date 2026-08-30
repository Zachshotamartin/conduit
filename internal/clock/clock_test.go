package clock_test

import (
	"reflect"
	"sync"
	"testing"
	"time"

	conduitclock "github.com/Zachshotamartin/conduit/internal/clock"
)

type architectureClock interface {
	Now() time.Time
	Schedule(d time.Duration, fn func(now time.Time)) conduitclock.TimerHandle
	Cancel(h conduitclock.TimerHandle) bool
}

var (
	_ architectureClock  = (conduitclock.Clock)(nil)
	_ conduitclock.Clock = (architectureClock)(nil)
	_ conduitclock.Clock = (*conduitclock.Fake)(nil)
	_ conduitclock.Clock = (*conduitclock.Real)(nil)
)

func TestClockInterfaceExactlyMatchesArchitectureContract(t *testing.T) {
	t.Parallel()

	got := reflect.TypeOf((*conduitclock.Clock)(nil)).Elem()
	want := reflect.TypeOf((*architectureClock)(nil)).Elem()
	if got.NumMethod() != 3 {
		t.Fatalf("Clock method count = %d, want exactly 3 (Now, Schedule, Cancel)", got.NumMethod())
	}
	for _, name := range []string{"Now", "Schedule", "Cancel"} {
		gotMethod, gotOK := got.MethodByName(name)
		wantMethod, wantOK := want.MethodByName(name)
		if !gotOK || !wantOK {
			t.Fatalf("Clock method %s presence: got=%t want=%t", name, gotOK, wantOK)
		}
		if gotMethod.Type != wantMethod.Type {
			t.Fatalf("Clock.%s signature = %s, want %s", name, gotMethod.Type, wantMethod.Type)
		}
	}
}

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

func TestScheduledCallbacksReceiveDeadlinesInDeterministicOrder(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	fake := conduitclock.NewFake(start)
	type firing struct {
		name       string
		argument   time.Time
		currentNow time.Time
	}
	var got []firing

	scheduleInjectedClock(fake, 3*time.Second, func(now time.Time) {
		got = append(got, firing{name: "third", argument: now, currentNow: fake.Now()})
	})
	scheduleInjectedClock(fake, time.Second, func(now time.Time) {
		got = append(got, firing{name: "first", argument: now, currentNow: fake.Now()})
	})
	scheduleInjectedClock(fake, 2*time.Second, func(now time.Time) {
		got = append(got, firing{name: "second", argument: now, currentNow: fake.Now()})
	})

	assertFirings(t, got, []firing(nil))
	fake.Advance(999 * time.Millisecond)
	assertFirings(t, got, []firing(nil))

	fake.Advance(time.Millisecond)
	assertFirings(t, got, []firing{
		{name: "first", argument: start.Add(time.Second), currentNow: start.Add(time.Second)},
	})

	fake.Advance(7 * time.Second)
	assertFirings(t, got, []firing{
		{name: "first", argument: start.Add(time.Second), currentNow: start.Add(time.Second)},
		{name: "second", argument: start.Add(2 * time.Second), currentNow: start.Add(2 * time.Second)},
		{name: "third", argument: start.Add(3 * time.Second), currentNow: start.Add(3 * time.Second)},
	})
	if gotNow, wantNow := fake.Now(), start.Add(8*time.Second); !gotNow.Equal(wantNow) {
		t.Fatalf("Now() after firing callbacks = %s, want final advance target %s", gotNow, wantNow)
	}
}

func TestScheduledCallbacksUseStableInsertionOrderForEqualDeadlines(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0).UTC()
	fake := conduitclock.NewFake(start)
	var got []int
	for i := 0; i < 8; i++ {
		i := i
		fake.Schedule(time.Second, func(now time.Time) {
			if want := start.Add(time.Second); !now.Equal(want) {
				t.Errorf("callback %d timestamp = %s, want %s", i, now, want)
			}
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
	handle := fake.Schedule(time.Second, func(time.Time) {
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

	firstHandle := firstClock.Schedule(time.Second, func(time.Time) {
		firstFired = true
	})
	secondHandle := secondClock.Schedule(time.Second, func(time.Time) {
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
	handle := fake.Schedule(time.Second, func(time.Time) {
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

func TestFakeClockConcurrentScheduleCancelAdvanceAndRead(t *testing.T) {
	t.Parallel()

	const timerCount = 64
	fake := conduitclock.NewFake(time.Unix(0, 0).UTC())
	handles := make([]conduitclock.TimerHandle, timerCount)
	var firedMu sync.Mutex
	fired := 0
	var scheduleGroup sync.WaitGroup
	for index := range handles {
		index := index
		scheduleGroup.Add(1)
		go func() {
			defer scheduleGroup.Done()
			handles[index] = fake.Schedule(time.Second, func(time.Time) {
				firedMu.Lock()
				fired++
				firedMu.Unlock()
			})
			_ = fake.Now()
		}()
	}
	scheduleGroup.Wait()

	for index := 0; index < timerCount; index += 2 {
		if !fake.Cancel(handles[index]) {
			t.Fatalf("Cancel(handle %d) = false, want true", index)
		}
	}
	fake.Advance(time.Second)

	firedMu.Lock()
	defer firedMu.Unlock()
	if fired != timerCount/2 {
		t.Fatalf("concurrent timer fire count = %d, want %d", fired, timerCount/2)
	}
}

func readInjectedClock(c conduitclock.Clock) time.Time {
	return c.Now()
}

func scheduleInjectedClock(c conduitclock.Clock, d time.Duration, fn func(time.Time)) conduitclock.TimerHandle {
	return c.Schedule(d, fn)
}

func cancelInjectedClock(c conduitclock.Clock, h conduitclock.TimerHandle) bool {
	return c.Cancel(h)
}

func assertFirings(t *testing.T, got, want any) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("firings = %#v, want %#v", got, want)
	}
}
