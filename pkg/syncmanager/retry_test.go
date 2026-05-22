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
