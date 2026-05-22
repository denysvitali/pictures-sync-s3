package syncmanager

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOp is a table-driven "mock rclone" operation: it records each attempt
// and returns a script of pre-canned outcomes (one per invocation). Once the
// script is exhausted it returns the last entry. This lets us exercise retry()
// deterministically without running real rclone subprocesses.
type fakeOpOutcome struct {
	err   error
	sleep time.Duration // optional pause before returning, to interleave with ctx cancel
}

type fakeOp struct {
	script   []fakeOpOutcome
	attempts int32
}

func (f *fakeOp) run(_ int) error {
	idx := int(atomic.AddInt32(&f.attempts, 1)) - 1
	if idx >= len(f.script) {
		idx = len(f.script) - 1
	}
	out := f.script[idx]
	if out.sleep > 0 {
		time.Sleep(out.sleep)
	}
	return out.err
}

// alwaysRetryable / neverRetryable are simple predicates for table tests.
func alwaysRetryable(error) bool { return true }
func neverRetryable(error) bool  { return false }

// TestRetryContextCancellationWraps verifies that when retry()'s context is
// cancelled BEFORE invoking op, the returned error wraps context.Canceled (or
// context.DeadlineExceeded) so downstream code can use errors.Is. Regression
// test for the previous fmt.Errorf("sync cancelled") form that erased the cause
// and forced callers (e.g. cardhandler.isCancellationError) to substring-match.
func TestRetryContextCancellationWraps(t *testing.T) {
	cases := []struct {
		name    string
		mkCtx   func() (context.Context, context.CancelFunc)
		wantErr error
		wantMsg string
	}{
		{
			name: "cancel_before_first_attempt",
			mkCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			wantErr: context.Canceled,
			wantMsg: "sync cancelled",
		},
		{
			name: "deadline_already_passed",
			mkCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				return ctx, cancel
			},
			wantErr: context.DeadlineExceeded,
			wantMsg: "sync cancelled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.mkCtx()
			defer cancel()

			op := &fakeOp{script: []fakeOpOutcome{{err: errors.New("should not be reached")}}}
			err := retry(ctx, op.run, alwaysRetryable, "test")

			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected errors.Is(err, %v) == true, got err=%v", tc.wantErr, err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error message %q should contain %q (legacy callers depend on this)", err.Error(), tc.wantMsg)
			}
			if got := atomic.LoadInt32(&op.attempts); got != 0 {
				t.Errorf("expected 0 attempts when ctx already cancelled, got %d", got)
			}
		})
	}
}

// TestRetryCancelDuringBackoff verifies that cancelling the context while
// retry() is waiting between attempts returns immediately with a wrapped
// context error.
func TestRetryCancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	op := &fakeOp{script: []fakeOpOutcome{{err: errors.New("net err")}}}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := retry(ctx, op.run, alwaysRetryable, "test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
	}
	if !strings.Contains(err.Error(), "sync cancelled") {
		t.Errorf("error message should contain 'sync cancelled' for cardhandler compatibility, got %q", err.Error())
	}
	if elapsed > 2*time.Second {
		t.Errorf("retry took %v to honor cancellation; expected sub-second", elapsed)
	}
	if got := atomic.LoadInt32(&op.attempts); got != 1 {
		t.Errorf("expected exactly 1 attempt before cancel, got %d", got)
	}
}

// TestRetryFirstExponentialDelayIsLongerThanInitial regression-tests a subtle
// off-by-one in the backoff schedule. retry() doubles `backoff` AFTER using it
// for the current attempt, so the value of `backoff` at the START of the first
// exponential attempt is what gets waited. Previously `backoff` was initialised
// to 5s, identical to initialDelay, which made attempt #4 wait 5s (same as
// attempts 1-3) and "exponential backoff" effectively start at attempt #5.
//
// The test runs through the initialRetries (3 x 5s fixed) and then measures
// the wait before the 4th-attempt retry. With the fix that wait is >= 9s; with
// the bug present it would be ~5s.
func TestRetryFirstExponentialDelayIsLongerThanInitial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-attempt timing test in -short mode")
	}

	// Use 4 failures so retry() enters the exponential branch on attempt 4.
	op := &fakeOp{script: []fakeOpOutcome{
		{err: errors.New("transient1")},
		{err: errors.New("transient2")},
		{err: errors.New("transient3")},
		{err: errors.New("transient4")},
		{err: nil}, // 5th attempt succeeds
	}}

	// We cancel just AFTER attempt 4 has been issued so retry() returns
	// during the backoff wait that follows attempt 4. The duration of that
	// wait minus the cancel-delay reveals whether the first exponential
	// step was 5s (buggy) or 10s (fixed). Easier: just measure total
	// elapsed and verify it exceeds the buggy schedule.
	//
	// Total time before attempt 5: 5 + 5 + 5 + firstExpDelay
	// Buggy: 5 + 5 + 5 + 5  = 20s
	// Fixed: 5 + 5 + 5 + 10 = 25s
	start := time.Now()
	err := retry(context.Background(), op.run, alwaysRetryable, "test")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success on attempt 5, got %v", err)
	}
	if got := atomic.LoadInt32(&op.attempts); got != 5 {
		t.Fatalf("expected 5 attempts, got %d", got)
	}
	// Allow scheduler slack. Buggy schedule would land near 20s; fixed
	// schedule lands near 25s. Anything >= 23s confirms the fix.
	if elapsed < 23*time.Second {
		t.Errorf("first exponential delay too short: total elapsed=%v, expected >= 23s (5+5+5+10)", elapsed)
	}
	if elapsed > 35*time.Second {
		t.Errorf("retry took unexpectedly long (%v); expected ~25s", elapsed)
	}
}

// TestRetryNonRetryableErrorReturnsImmediately ensures non-retryable errors
// short-circuit the retry loop and the original error is returned (not the
// "failed after N attempts" wrapper).
func TestRetryNonRetryableErrorReturnsImmediately(t *testing.T) {
	sentinel := errors.New("hard error")
	op := &fakeOp{script: []fakeOpOutcome{{err: sentinel}, {err: nil}}}

	err := retry(context.Background(), op.run, neverRetryable, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if got := atomic.LoadInt32(&op.attempts); got != 1 {
		t.Errorf("expected exactly 1 attempt for non-retryable err, got %d", got)
	}
}
