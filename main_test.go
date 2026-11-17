package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type result struct {
	remediations []time.Duration // elapsed time at each remediation attempt
	kills        []time.Duration // elapsed time at each kill attempt
	ctxErr       error
}

// run drives monitor single-threaded with the default timings and a fake clock that advances 30s on
// every now() call after the first (monitor calls it once for the start and once per tick that finds
// addresses missing). present reports whether the addresses exist, given the remediations so far.
// The context is only cancelled by a safety timeout, never by the mocks.
func run(t *testing.T, present func(remediations int, elapsed time.Duration) bool,
	remediate func(attempt int) error, kill func(attempt int) bool,
) result {
	t.Helper()

	remediateAfter, terminationTimeout = time.Minute, 3*time.Minute

	const maxTicks = 20

	ticks := make(chan time.Time, maxTicks)
	for i := 0; i < maxTicks; i++ {
		ticks <- time.Time{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var (
		elapsed time.Duration
		calls   int
		res     result
	)

	now := func() time.Time {
		if calls > 0 {
			elapsed += 30 * time.Second
		}

		calls++

		return time.Unix(0, 0).Add(elapsed)
	}

	monitor(ctx, ticks, now,
		func() bool { return present(len(res.remediations), elapsed) },
		func(context.Context) error {
			res.remediations = append(res.remediations, elapsed)

			return remediate(len(res.remediations))
		},
		func() bool {
			res.kills = append(res.kills, elapsed)

			return kill(len(res.kills))
		},
	)

	res.ctxErr = ctx.Err()

	return res
}

func never(int, time.Duration) bool { return false }
func okRemediate(int) error         { return nil }
func okKill(int) bool               { return true }

func equal(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func secs(s ...int) (d []time.Duration) {
	for _, v := range s {
		d = append(d, time.Duration(v)*time.Second)
	}

	return d
}

func TestAddressesPresent(t *testing.T) {
	r := run(t, func(int, time.Duration) bool { return true }, okRemediate, okKill)
	if len(r.remediations) != 0 || len(r.kills) != 0 {
		t.Fatalf("remediations=%v kills=%v, want none", r.remediations, r.kills)
	}
}

func TestNoRemediationBeforeRemediateAfter(t *testing.T) {
	r := run(t, func(_ int, e time.Duration) bool { return e >= 30*time.Second }, okRemediate, okKill)
	if len(r.remediations) != 0 || len(r.kills) != 0 {
		t.Fatalf("remediations=%v kills=%v, want none", r.remediations, r.kills)
	}
}

func TestRemediationFixes(t *testing.T) {
	r := run(t, func(rem int, _ time.Duration) bool { return rem > 0 }, okRemediate, okKill)
	if !equal(r.remediations, secs(60)) || len(r.kills) != 0 {
		t.Fatalf("remediations=%v kills=%v, want [60s] and no kill", r.remediations, r.kills)
	}
}

func TestKillAfterTerminationTimeout(t *testing.T) {
	r := run(t, never, okRemediate, okKill)
	if !equal(r.remediations, secs(60, 90, 120, 150)) || !equal(r.kills, secs(180)) {
		t.Fatalf("remediations=%v kills=%v, want 60/90/120/150s and kill at 180s", r.remediations, r.kills)
	}
}

func TestSuccessfulKillStopsLoop(t *testing.T) {
	r := run(t, never, okRemediate, okKill)
	if len(r.kills) != 1 || r.ctxErr != nil {
		t.Fatalf("kills=%v ctxErr=%v, want a single kill and the loop to return on its own", r.kills, r.ctxErr)
	}
}

func TestFailedKillRetriesOnNextTick(t *testing.T) {
	r := run(t, never, okRemediate, func(attempt int) bool { return attempt >= 2 })
	if !equal(r.kills, secs(180, 210)) || r.ctxErr != nil {
		t.Fatalf("kills=%v ctxErr=%v, want kills at 180s and 210s", r.kills, r.ctxErr)
	}
}

func TestRemediationFailureCountsAndContinues(t *testing.T) {
	r := run(t, never, func(int) error { return errors.New("command terminated with exit code 1") }, okKill)
	if !equal(r.remediations, secs(60, 90, 120, 150)) || !equal(r.kills, secs(180)) {
		t.Fatalf("remediations=%v kills=%v, want 4 failed attempts then a kill", r.remediations, r.kills)
	}
}
