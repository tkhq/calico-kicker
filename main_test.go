package main

import (
	"context"
	"testing"
	"time"
)

// run drives monitor single-threaded with a fake clock that advances 30s on every now() call after
// the first (monitor calls it once for the start and once per tick that finds addresses missing).
// present reports whether the addresses exist given the number of remediations so far and the elapsed time.
func run(t *testing.T, present func(remediations int, elapsed time.Duration) bool) (remediations, kills int) {
	t.Helper()

	allocateAfter, terminationTimeout = time.Minute, 3*time.Minute

	const maxTicks = 20

	ticks := make(chan time.Time, maxTicks)
	for i := 0; i < maxTicks; i++ {
		ticks <- time.Time{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var elapsed time.Duration

	calls := 0
	now := func() time.Time {
		if calls > 0 {
			elapsed += 30 * time.Second
		}

		calls++

		return time.Unix(0, 0).Add(elapsed)
	}

	monitor(ctx, ticks, now,
		func() bool { return present(remediations, elapsed) },
		func(context.Context) error { remediations++; return nil },
		func() bool { kills++; cancel(); return true },
	)

	return remediations, kills
}

func TestAddressesPresent(t *testing.T) {
	r, k := run(t, func(int, time.Duration) bool { return true })
	if r != 0 || k != 0 {
		t.Fatalf("remediations=%d kills=%d, want 0/0", r, k)
	}
}

func TestMissingUntilAfterAllocateAfterThenFixed(t *testing.T) {
	// Present only once the allocator has run; before allocate-after nothing may run.
	r, k := run(t, func(rem int, _ time.Duration) bool { return rem > 0 })
	if r != 1 || k != 0 {
		t.Fatalf("remediations=%d kills=%d, want 1/0", r, k)
	}
}

func TestNoRemediationBeforeAllocateAfter(t *testing.T) {
	// Addresses appear by themselves at 30s, before allocate-after.
	r, k := run(t, func(_ int, e time.Duration) bool { return e >= 30*time.Second })
	if r != 0 || k != 0 {
		t.Fatalf("remediations=%d kills=%d, want 0/0", r, k)
	}
}

func TestKillAfterTerminationTimeout(t *testing.T) {
	r, k := run(t, func(int, time.Duration) bool { return false })
	if r < 2 || k != 1 {
		t.Fatalf("remediations=%d kills=%d, want >=2/1", r, k)
	}
}
