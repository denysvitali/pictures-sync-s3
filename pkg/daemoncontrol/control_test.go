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
	startTestServer(t, func(context.Context, string) Response {
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

func TestRequestManualSync_WithDevicePath(t *testing.T) {
	withTempSocket(t)

	const wantedDevicePath = "/dev/sda1"
	called := false
	var requestedPath string
	startTestServer(t, func(_ context.Context, devicePath string) Response {
		called = true
		requestedPath = devicePath
		return OK("accepted")
	})

	if err := RequestManualSyncWithPath(context.Background(), wantedDevicePath); err != nil {
		t.Fatalf("RequestManualSyncWithPath failed: %v", err)
	}
	if !called {
		t.Fatal("Expected manual sync handler to be called")
	}
	if requestedPath != wantedDevicePath {
		t.Fatalf("Expected device path %q, got %q", wantedDevicePath, requestedPath)
	}
}

func TestRequestManualSync_ErrorResponse(t *testing.T) {
	withTempSocket(t)

	startTestServer(t, func(context.Context, string) Response {
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
		errCh <- Serve(ctx, func(context.Context, string) Response {
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

func TestRequestFormatSDCard_WithDevicePath(t *testing.T) {
	withTempSocket(t)

	const wantedDevicePath = "/dev/sda1"
	const wantedLabel = "CAMERA_1"
	called := false
	var requestedPath string
	var requestedLabel string

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeWithHandlers(ctx, Handlers{
			ManualSync: func(context.Context, string) Response { return OK("accepted") },
			CancelSync: func(context.Context) Response { return OK("cancelled") },
			FormatSDCard: func(_ context.Context, devicePath, label string) Response {
				called = true
				requestedPath = devicePath
				requestedLabel = label
				return OK("formatted")
			},
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

	if err := RequestFormatSDCard(context.Background(), wantedDevicePath, wantedLabel); err != nil {
		t.Fatalf("RequestFormatSDCard failed: %v", err)
	}
	if !called {
		t.Fatal("Expected format handler to be called")
	}
	if requestedPath != wantedDevicePath {
		t.Fatalf("Expected device path %q, got %q", wantedDevicePath, requestedPath)
	}
	if requestedLabel != wantedLabel {
		t.Fatalf("Expected label %q, got %q", wantedLabel, requestedLabel)
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
