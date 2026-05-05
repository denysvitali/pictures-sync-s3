package daemoncontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	CommandManualSync = "manual_sync"
	CommandCancelSync = "cancel_sync"

	CodeNoSDCardMounted   = "no_sd_card_mounted"
	CodeSyncAlreadyActive = "sync_already_active"
	CodeUnavailable       = "daemon_unavailable"
	CodeInternalError     = "internal_error"

	requestTimeout = 5 * time.Second
	socketEnv      = "PICTURES_SYNC_DAEMON_SOCKET"
)

type Request struct {
	Command    string `json:"command"`
	DevicePath string `json:"device_path,omitempty"`
}

type Response struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type CommandError struct {
	Code    string
	Message string
}

func (e *CommandError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type ManualSyncHandler func(context.Context, string) Response
type CancelSyncHandler func(context.Context) Response

func SocketPath() string {
	if socketPath := os.Getenv(socketEnv); socketPath != "" {
		return socketPath
	}
	return filepath.Join(os.TempDir(), "pictures-sync", "daemon.sock")
}

func OK(message string) Response {
	return Response{Status: "ok", Message: message}
}

func Error(code, message string) Response {
	return Response{Status: "error", Code: code, Message: message}
}

func Serve(ctx context.Context, handleManualSync ManualSyncHandler, handleCancelSync CancelSyncHandler) error {
	if handleManualSync == nil {
		return errors.New("manual sync handler is required")
	}
	if handleCancelSync == nil {
		return errors.New("cancel sync handler is required")
	}

	socketPath := SocketPath()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return fmt.Errorf("create daemon control directory: %w", err)
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale daemon control socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on daemon control socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	if err := os.Chmod(socketPath, 0660); err != nil {
		return fmt.Errorf("set daemon control socket permissions: %w", err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept daemon control connection: %w", err)
		}

		go handleConn(ctx, conn, handleManualSync, handleCancelSync)
	}
}

func handleConn(ctx context.Context, conn net.Conn, handleManualSync ManualSyncHandler, handleCancelSync CancelSyncHandler) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(requestTimeout))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Error(CodeInternalError, "invalid daemon control request"))
		return
	}

	switch req.Command {
	case CommandManualSync:
		_ = json.NewEncoder(conn).Encode(handleManualSync(ctx, req.DevicePath))
	case CommandCancelSync:
		_ = json.NewEncoder(conn).Encode(handleCancelSync(ctx))
	default:
		_ = json.NewEncoder(conn).Encode(Error(CodeInternalError, "unknown daemon control command"))
	}
}

func RequestManualSync(ctx context.Context) error {
	return sendCommand(ctx, CommandManualSync, "")
}

func RequestManualSyncWithPath(ctx context.Context, devicePath string) error {
	return sendCommand(ctx, CommandManualSync, devicePath)
}

func RequestCancelSync(ctx context.Context) error {
	return sendCommand(ctx, CommandCancelSync, "")
}

func sendCommand(ctx context.Context, command string, devicePath string) error {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(requestCtx, "unix", SocketPath())
	if err != nil {
		return &CommandError{
			Code:    CodeUnavailable,
			Message: fmt.Sprintf("pictures-sync daemon control socket is unavailable: %v", err),
		}
	}
	defer conn.Close()

	deadline := time.Now().Add(requestTimeout)
	if ctxDeadline, ok := requestCtx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if err := json.NewEncoder(conn).Encode(Request{Command: command, DevicePath: devicePath}); err != nil {
		return &CommandError{Code: CodeUnavailable, Message: fmt.Sprintf("send daemon command: %v", err)}
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return &CommandError{Code: CodeUnavailable, Message: fmt.Sprintf("read daemon response: %v", err)}
	}

	if resp.Status != "ok" {
		return &CommandError{Code: resp.Code, Message: resp.Message}
	}

	return nil
}
