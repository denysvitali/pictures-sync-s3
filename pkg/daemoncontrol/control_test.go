package daemoncontrol

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempSocket(t *testing.T) {
	t.Helper()

	t.Setenv(socketEnv, filepath.Join(t.TempDir(), "daemon.sock"))
}

func startTestServer(t *testing.T, handler ManualSyncHandler) context.CancelFunc {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, handler, func(context.Context) Response {
			return OK("cancelled")
		})
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(SocketPath()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("daemon control socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Serve returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve did not stop")
		}
	})

	return cancel
}

func TestRequestManualSync_OK(t *testing.T) {
	withTempSocket(t)

	called := false
	startTestServer(t, func(context.Context) Response {
		called = true
		return OK("accepted")
	})

	if err := RequestManualSync(context.Background()); err != nil {
		t.Fatalf("RequestManualSync failed: %v", err)
	}
	if !called {
		t.Fatal("Expected manual sync handler to be called")
	}
}

func TestRequestManualSync_ErrorResponse(t *testing.T) {
	withTempSocket(t)

	startTestServer(t, func(context.Context) Response {
		return Error(CodeNoSDCardMounted, "no SD card mounted")
	})

	err := RequestManualSync(context.Background())
	if err == nil {
		t.Fatal("Expected error")
	}

	commandErr, ok := err.(*CommandError)
	if !ok {
		t.Fatalf("Expected CommandError, got %T", err)
	}
	if commandErr.Code != CodeNoSDCardMounted {
		t.Fatalf("Expected code %q, got %q", CodeNoSDCardMounted, commandErr.Code)
	}
}

func TestRequestManualSync_Unavailable(t *testing.T) {
	withTempSocket(t)

	err := RequestManualSync(context.Background())
	if err == nil {
		t.Fatal("Expected error")
	}

	commandErr, ok := err.(*CommandError)
	if !ok {
		t.Fatalf("Expected CommandError, got %T", err)
	}
	if commandErr.Code != CodeUnavailable {
		t.Fatalf("Expected code %q, got %q", CodeUnavailable, commandErr.Code)
	}
}

func TestRequestCancelSync_OK(t *testing.T) {
	withTempSocket(t)

	called := false
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, func(context.Context) Response {
			return OK("accepted")
		}, func(context.Context) Response {
			called = true
			return OK("cancelled")
		})
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(SocketPath()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("daemon control socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := RequestCancelSync(context.Background()); err != nil {
		t.Fatalf("RequestCancelSync failed: %v", err)
	}
	if !called {
		t.Fatal("Expected cancel sync handler to be called")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
}
