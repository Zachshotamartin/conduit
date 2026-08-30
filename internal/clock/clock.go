package clock

import (
	"container/heap"
	"errors"
	"sync"
	"time"
)

// ErrNegativeAdvance is the value panicked by Fake.Advance when asked to
// move backwards. Fake time is monotonic: callers must create a new Fake to
// model an earlier instant.
var ErrNegativeAdvance = errors.New("clock: negative advance")

// Clock is the sole time source used by Conduit code. Implementations own
// both time reads and timer creation so callers never depend directly on the
// process wall clock. Scheduled callbacks must be non-blocking.
type Clock interface {
	Now() time.Time
	Schedule(d time.Duration, fn func(now time.Time)) TimerHandle
	Cancel(h TimerHandle) bool
}

// TimerHandle identifies one scheduled callback on the Clock that created
// it. It is an opaque, comparable value. Its zero value is invalid, and a
// handle from one Clock cannot cancel a timer on another Clock.
type TimerHandle struct {
	owner *timerOwner
	id    uint64
}

// timerOwner deliberately has non-zero size so distinct allocations have
// distinct addresses. TimerHandle uses its address as the clock identity.
type timerOwner struct {
	_ byte
}

// Real is the process-wall-clock implementation of Clock. Its zero value is
// ready for use. A Real must not be copied after first use.
//
// Per-connection production deadlines move to the shared timing wheel in R2;
// Real keeps all direct standard-library time access inside this package in
// the meantime.
type Real struct {
	mu     sync.Mutex
	owner  *timerOwner
	nextID uint64
	timers map[uint64]*time.Timer
}

// NewReal returns a Clock backed by the process wall clock.
func NewReal() *Real {
	return &Real{}
}

// Now returns the current process-wall-clock time.
func (*Real) Now() time.Time {
	return time.Now()
}

// Schedule arranges for fn to run once after d. Wall-clock callbacks run in
// their own goroutine, matching time.AfterFunc semantics, and receive the
// current firing instant. Callbacks must be non-blocking.
func (r *Real) Schedule(d time.Duration, fn func(now time.Time)) TimerHandle {
	if fn == nil {
		panic("clock: nil callback")
	}

	r.mu.Lock()
	r.initializeLocked()
	h := TimerHandle{owner: r.owner, id: r.nextHandleIDLocked()}
	timer := time.AfterFunc(d, func() {
		r.mu.Lock()
		if _, pending := r.timers[h.id]; !pending {
			r.mu.Unlock()
			return
		}
		delete(r.timers, h.id)
		r.mu.Unlock()

		fn(time.Now())
	})
	r.timers[h.id] = timer
	r.mu.Unlock()

	return h
}

// Cancel prevents a pending callback from running. It reports false for a
// zero, foreign, cancelled, firing, or already-fired handle.
func (r *Real) Cancel(h TimerHandle) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.owner == nil || h.owner != r.owner || h.id == 0 {
		return false
	}
	timer, pending := r.timers[h.id]
	if !pending || !timer.Stop() {
		return false
	}
	delete(r.timers, h.id)
	return true
}

func (r *Real) initializeLocked() {
	if r.owner == nil {
		r.owner = &timerOwner{}
	}
	if r.timers == nil {
		r.timers = make(map[uint64]*time.Timer)
	}
}

func (r *Real) nextHandleIDLocked() uint64 {
	r.nextID++
	if r.nextID == 0 {
		panic("clock: timer handle space exhausted")
	}
	return r.nextID
}

// Fake is a deterministic, concurrency-safe Clock for tests. Time changes
// only through Advance. Scheduled callbacks run synchronously in deadline
// order during Advance; equal deadlines retain insertion order.
//
// Scheduled callbacks may call Now, Schedule, and Cancel. They must be
// non-blocking and must not call Advance recursively.
type Fake struct {
	advanceMu sync.Mutex
	mu        sync.Mutex
	now       time.Time
	owner     *timerOwner
	nextID    uint64
	nextOrder uint64
	timers    fakeTimerHeap
	byID      map[uint64]*fakeTimer
}

// NewFake returns a deterministic clock positioned at start.
func NewFake(start time.Time) *Fake {
	f := &Fake{
		now:   start,
		owner: &timerOwner{},
		byID:  make(map[uint64]*fakeTimer),
	}
	heap.Init(&f.timers)
	return f
}

// Now returns the fake clock's current instant.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Schedule adds a one-shot callback. Non-positive durations are scheduled
// at the current fake instant and fire on the next Advance, including
// Advance(0); scheduling alone never fires a callback. The callback receives
// its exact scheduled deadline and must be non-blocking.
func (f *Fake) Schedule(d time.Duration, fn func(now time.Time)) TimerHandle {
	if fn == nil {
		panic("clock: nil callback")
	}
	if d < 0 {
		d = 0
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.initializeLocked()

	f.nextID++
	if f.nextID == 0 {
		panic("clock: timer handle space exhausted")
	}
	f.nextOrder++
	if f.nextOrder == 0 {
		panic("clock: timer ordering space exhausted")
	}

	h := TimerHandle{owner: f.owner, id: f.nextID}
	timer := &fakeTimer{
		handle:   h,
		deadline: f.now.Add(d),
		order:    f.nextOrder,
		fn:       fn,
		index:    -1,
	}
	heap.Push(&f.timers, timer)
	f.byID[h.id] = timer
	return h
}

// Cancel removes a pending callback. It reports false for a zero, foreign,
// cancelled, firing, or already-fired handle.
func (f *Fake) Cancel(h TimerHandle) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.owner == nil || h.owner != f.owner || h.id == 0 {
		return false
	}
	timer, pending := f.byID[h.id]
	if !pending {
		return false
	}
	heap.Remove(&f.timers, timer.index)
	delete(f.byID, h.id)
	return true
}

// Advance moves fake time forward by d and synchronously fires every timer
// due at or before the target. Callback-time Now calls observe that timer's
// deadline; when Advance returns, Now equals the target.
//
// Advance panics with ErrNegativeAdvance when d is negative. Concurrent
// Advance calls are serialized and therefore add their durations in full.
func (f *Fake) Advance(d time.Duration) {
	if d < 0 {
		panic(ErrNegativeAdvance)
	}

	f.advanceMu.Lock()
	defer f.advanceMu.Unlock()

	f.mu.Lock()
	f.initializeLocked()
	target := f.now.Add(d)
	for f.timers.Len() != 0 {
		next := f.timers[0]
		if next.deadline.After(target) {
			break
		}

		heap.Pop(&f.timers)
		delete(f.byID, next.handle.id)
		f.now = next.deadline
		fn := next.fn

		f.mu.Unlock()
		fn(next.deadline)
		f.mu.Lock()
	}
	f.now = target
	f.mu.Unlock()
}

func (f *Fake) initializeLocked() {
	if f.owner == nil {
		f.owner = &timerOwner{}
	}
	if f.byID == nil {
		f.byID = make(map[uint64]*fakeTimer)
	}
}

type fakeTimer struct {
	handle   TimerHandle
	deadline time.Time
	order    uint64
	fn       func(time.Time)
	index    int
}

type fakeTimerHeap []*fakeTimer

func (h fakeTimerHeap) Len() int {
	return len(h)
}

func (h fakeTimerHeap) Less(i, j int) bool {
	if h[i].deadline.Equal(h[j].deadline) {
		return h[i].order < h[j].order
	}
	return h[i].deadline.Before(h[j].deadline)
}

func (h fakeTimerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *fakeTimerHeap) Push(value any) {
	timer := value.(*fakeTimer)
	timer.index = len(*h)
	*h = append(*h, timer)
}

func (h *fakeTimerHeap) Pop() any {
	old := *h
	last := len(old) - 1
	timer := old[last]
	old[last] = nil
	timer.index = -1
	*h = old[:last]
	return timer
}

var (
	_ Clock = (*Real)(nil)
	_ Clock = (*Fake)(nil)
)
