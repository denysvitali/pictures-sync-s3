package daemoncontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdcardbrowser"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

const (
	CommandManualSync      = "manual_sync"
	CommandCancelSync      = "cancel_sync"
	CommandStatus          = "status"
	CommandHistory         = "history"
	CommandDevices         = "devices"
	CommandFormatSDCard    = "format_sdcard"
	CommandRedetectSDCard  = "redetect_sdcard"
	CommandSDCardFiles     = "sdcard_files"
	CommandSDCardPreview   = "sdcard_preview"
	CommandSDCardThumbnail = "sdcard_thumbnail"
	CommandSubscribe       = "subscribe"

	// EnvelopeKindState carries a full CurrentState snapshot.
	EnvelopeKindState = "state"
	// EnvelopeKindEvent carries a single Event.
	EnvelopeKindEvent = "event"

	CodeNoSDCardMounted   = "no_sd_card_mounted"
	CodeSyncAlreadyActive = "sync_already_active"
	CodeInvalidDevice     = "invalid_device"
	CodeUnavailable       = "daemon_unavailable"
	CodeInternalError     = "internal_error"

	requestTimeout       = 5 * time.Second
	formatRequestTimeout = 2*time.Minute + 5*time.Second
	socketEnv            = "PICTURES_SYNC_DAEMON_SOCKET"

	// subscribeWriteTimeout caps how long a single envelope write can block
	// before the streaming handler tears down the connection. It also doubles
	// as the per-iteration keepalive deadline ceiling.
	subscribeWriteTimeout = 30 * time.Second
)

type Request struct {
	Command    string `json:"command"`
	DevicePath string `json:"device_path,omitempty"`
	Label      string `json:"label,omitempty"`
	Path       string `json:"path,omitempty"`
}

type Response struct {
	Status  string          `json:"status"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Envelope is a tagged push message used for the subscribe stream. Exactly
// one of State or Event will be populated depending on Kind. Future kinds may
// be added without breaking older clients (they should ignore unknown kinds).
type Envelope struct {
	Kind  string               `json:"kind"`
	State *state.CurrentState  `json:"state,omitempty"`
	Event *events.Event        `json:"event,omitempty"`
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
type StatusHandler func(context.Context) Response
type HistoryHandler func(context.Context) Response
type DevicesHandler func(context.Context) Response
type FormatSDCardHandler func(context.Context, string, string) Response
type RedetectSDCardHandler func(context.Context) Response
type SDCardFilesHandler func(context.Context, string) Response
type SDCardPreviewHandler func(context.Context, string) Response
type SDCardThumbnailHandler func(context.Context, string) Response

// SubscribeHandler streams Envelopes to the given out channel until ctx is
// cancelled. Implementations should send an initial state snapshot on connect
// and then forward subsequent state updates and events as they happen. The
// channel writer (the server-side dispatcher) takes care of serialising
// envelopes to the socket — handlers must not close out themselves; returning
// from the handler signals completion.
type SubscribeHandler func(ctx context.Context, out chan<- Envelope)

type Handlers struct {
	ManualSync      ManualSyncHandler
	CancelSync      CancelSyncHandler
	Status          StatusHandler
	History         HistoryHandler
	Devices         DevicesHandler
	FormatSDCard    FormatSDCardHandler
	RedetectSDCard  RedetectSDCardHandler
	SDCardFiles     SDCardFilesHandler
	SDCardPreview   SDCardPreviewHandler
	SDCardThumbnail SDCardThumbnailHandler
	Subscribe       SubscribeHandler
}

func SocketPath() string {
	if socketPath := os.Getenv(socketEnv); socketPath != "" {
		return socketPath
	}
	return filepath.Join(os.TempDir(), "pictures-sync", "daemon.sock")
}

func OK(message string) Response {
	return Response{Status: "ok", Message: message}
}

func OKData(message string, data interface{}) Response {
	encoded, err := json.Marshal(data)
	if err != nil {
		return Error(CodeInternalError, fmt.Sprintf("encode daemon response: %v", err))
	}
	return Response{Status: "ok", Message: message, Data: encoded}
}

func Error(code, message string) Response {
	return Response{Status: "error", Code: code, Message: message}
}

func Serve(ctx context.Context, handleManualSync ManualSyncHandler, handleCancelSync CancelSyncHandler) error {
	return ServeWithHandlers(ctx, Handlers{
		ManualSync: handleManualSync,
		CancelSync: handleCancelSync,
	})
}

func ServeWithHandlers(ctx context.Context, handlers Handlers) error {
	if handlers.ManualSync == nil {
		return errors.New("manual sync handler is required")
	}
	if handlers.CancelSync == nil {
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

		go handleConn(ctx, conn, handlers)
	}
}

func handleConn(ctx context.Context, conn net.Conn, handlers Handlers) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(requestTimeout))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Error(CodeInternalError, "invalid daemon control request"))
		return
	}
	if req.Command == CommandFormatSDCard {
		_ = conn.SetDeadline(time.Now().Add(formatRequestTimeout))
	}
	if req.Command == CommandSubscribe {
		// Streaming subscriptions are long-lived; clear the per-request
		// deadline. handleSubscribe enforces a per-write deadline instead so
		// dead connections still get torn down promptly.
		_ = conn.SetDeadline(time.Time{})
	}

	switch req.Command {
	case CommandManualSync:
		_ = json.NewEncoder(conn).Encode(handlers.ManualSync(ctx, req.DevicePath))
	case CommandCancelSync:
		_ = json.NewEncoder(conn).Encode(handlers.CancelSync(ctx))
	case CommandStatus:
		_ = json.NewEncoder(conn).Encode(call0(ctx, handlers.Status))
	case CommandHistory:
		_ = json.NewEncoder(conn).Encode(call0(ctx, handlers.History))
	case CommandDevices:
		_ = json.NewEncoder(conn).Encode(call0(ctx, handlers.Devices))
	case CommandFormatSDCard:
		_ = json.NewEncoder(conn).Encode(callFormatSDCard(ctx, handlers.FormatSDCard, req.DevicePath, req.Label))
	case CommandRedetectSDCard:
		_ = json.NewEncoder(conn).Encode(call0(ctx, handlers.RedetectSDCard))
	case CommandSDCardFiles:
		_ = json.NewEncoder(conn).Encode(callPath(ctx, handlers.SDCardFiles, req.Path))
	case CommandSDCardPreview:
		_ = json.NewEncoder(conn).Encode(callPath(ctx, handlers.SDCardPreview, req.Path))
	case CommandSDCardThumbnail:
		_ = json.NewEncoder(conn).Encode(callPath(ctx, handlers.SDCardThumbnail, req.Path))
	case CommandSubscribe:
		handleSubscribe(ctx, conn, handlers.Subscribe)
	default:
		_ = json.NewEncoder(conn).Encode(Error(CodeInternalError, "unknown daemon control command"))
	}
}

// handleSubscribe runs the streaming subscribe loop for one client conn. It
// hands a buffered envelope channel to the registered SubscribeHandler (which
// owns producing snapshots and updates) and serialises each envelope to the
// socket as newline-delimited JSON. The function returns when ctx is cancelled,
// the handler returns, or a write fails.
func handleSubscribe(ctx context.Context, conn net.Conn, handler SubscribeHandler) {
	if handler == nil {
		_ = json.NewEncoder(conn).Encode(Error(CodeUnavailable, "subscribe is not available"))
		return
	}

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered so the producer can keep pushing while we serialise. The
	// SubscribeHandler is expected to drop on full channel rather than block.
	envelopes := make(chan Envelope, 64)

	go func() {
		defer close(envelopes)
		handler(subCtx, envelopes)
	}()

	// Tear down the subscription as soon as the client closes the socket so
	// the handler goroutine and its state.Manager/events.Manager subscriptions
	// don't leak. A short read with a non-zero buffer returns EOF (or any
	// error) when the peer hangs up; we treat any read result as a signal to
	// stop.
	go func() {
		buf := make([]byte, 1)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				cancel()
				return
			}
			// Drain any unexpected client bytes - subscribe is one-way after
			// the initial request - but keep the loop alive until EOF/err.
		}
	}()

	// json.Encoder appends a trailing newline after each value, giving us
	// newline-delimited JSON for free. Wrap in bufio.Writer so encoding stays
	// efficient for back-to-back envelopes; flush after each write so clients
	// see updates immediately.
	writer := bufio.NewWriter(conn)
	enc := json.NewEncoder(writer)

	for {
		select {
		case <-subCtx.Done():
			return
		case env, ok := <-envelopes:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(subscribeWriteTimeout))
			if err := enc.Encode(env); err != nil {
				return
			}
			if err := writer.Flush(); err != nil {
				return
			}
		}
	}
}

func call0(ctx context.Context, handler func(context.Context) Response) Response {
	if handler == nil {
		return Error(CodeUnavailable, "daemon command is not available")
	}
	return handler(ctx)
}

func callPath(ctx context.Context, handler func(context.Context, string) Response, path string) Response {
	if handler == nil {
		return Error(CodeUnavailable, "daemon command is not available")
	}
	return handler(ctx, path)
}

func callFormatSDCard(ctx context.Context, handler FormatSDCardHandler, devicePath, label string) Response {
	if handler == nil {
		return Error(CodeUnavailable, "daemon command is not available")
	}
	return handler(ctx, devicePath, label)
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

func RequestStatus(ctx context.Context) (state.CurrentState, error) {
	var status state.CurrentState
	err := sendCommandData(ctx, Request{Command: CommandStatus}, &status)
	return status, err
}

func RequestHistory(ctx context.Context) ([]state.SyncRecord, error) {
	var history []state.SyncRecord
	err := sendCommandData(ctx, Request{Command: CommandHistory}, &history)
	return history, err
}

func RequestDevices(ctx context.Context) ([]sdmonitor.DeviceInfo, error) {
	var devices []sdmonitor.DeviceInfo
	err := sendCommandData(ctx, Request{Command: CommandDevices}, &devices)
	return devices, err
}

func RequestFormatSDCard(ctx context.Context, devicePath, label string) error {
	_, err := sendRequestWithTimeout(ctx, Request{
		Command:    CommandFormatSDCard,
		DevicePath: devicePath,
		Label:      label,
	}, formatRequestTimeout)
	return err
}

func RequestRedetectSDCard(ctx context.Context) error {
	return sendCommand(ctx, CommandRedetectSDCard, "")
}

func RequestSDCardFiles(ctx context.Context, path string) (*sdcardbrowser.FileList, error) {
	var result sdcardbrowser.FileList
	if err := sendCommandData(ctx, Request{Command: CommandSDCardFiles, Path: path}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func RequestSDCardPreview(ctx context.Context, path string) (*sdcardbrowser.Preview, error) {
	var result sdcardbrowser.Preview
	if err := sendCommandData(ctx, Request{Command: CommandSDCardPreview, Path: path}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func RequestSDCardThumbnail(ctx context.Context, path string) (*sdcardbrowser.Preview, error) {
	var result sdcardbrowser.Preview
	if err := sendCommandData(ctx, Request{Command: CommandSDCardThumbnail, Path: path}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func sendCommand(ctx context.Context, command string, devicePath string) error {
	_, err := sendRequest(ctx, Request{Command: command, DevicePath: devicePath})
	return err
}

func sendCommandData(ctx context.Context, req Request, out interface{}) error {
	resp, err := sendRequest(ctx, req)
	if err != nil {
		return err
	}
	if len(resp.Data) == 0 {
		return &CommandError{Code: CodeInternalError, Message: "daemon response did not include data"}
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return &CommandError{Code: CodeInternalError, Message: fmt.Sprintf("decode daemon response: %v", err)}
	}
	return nil
}

// Subscribe opens a streaming connection to the daemon control socket and
// returns a channel that emits Envelopes as they arrive. The channel is
// closed when ctx is cancelled, the connection drops, or the daemon ends
// the stream. Callers are responsible for reconnect logic — this method
// performs a single dial and does not retry on failure.
func Subscribe(ctx context.Context) (<-chan Envelope, error) {
	var dialer net.Dialer
	dialCtx, cancelDial := context.WithTimeout(ctx, requestTimeout)
	defer cancelDial()
	conn, err := dialer.DialContext(dialCtx, "unix", SocketPath())
	if err != nil {
		return nil, &CommandError{
			Code:    CodeUnavailable,
			Message: fmt.Sprintf("pictures-sync daemon control socket is unavailable: %v", err),
		}
	}

	// Send the subscribe request with a short write deadline; afterwards we
	// rely on ctx and natural read EOF for tear-down.
	_ = conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	if err := json.NewEncoder(conn).Encode(Request{Command: CommandSubscribe}); err != nil {
		conn.Close()
		return nil, &CommandError{Code: CodeUnavailable, Message: fmt.Sprintf("send daemon subscribe: %v", err)}
	}
	_ = conn.SetWriteDeadline(time.Time{})
	_ = conn.SetReadDeadline(time.Time{})

	out := make(chan Envelope, 16)

	go func() {
		defer close(out)
		defer conn.Close()

		// Close the conn when ctx is cancelled so the blocking decode returns.
		ctxDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-ctxDone:
			}
		}()
		defer close(ctxDone)

		dec := json.NewDecoder(bufio.NewReader(conn))
		for {
			var env Envelope
			if err := dec.Decode(&env); err != nil {
				if errors.Is(err, io.EOF) || ctx.Err() != nil {
					return
				}
				// Any other decode error also terminates the stream; the
				// caller should reconnect.
				return
			}
			select {
			case out <- env:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func sendRequest(ctx context.Context, req Request) (Response, error) {
	return sendRequestWithTimeout(ctx, req, requestTimeout)
}

func sendRequestWithTimeout(ctx context.Context, req Request, timeout time.Duration) (Response, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var resp Response
	var dialer net.Dialer
	conn, err := dialer.DialContext(requestCtx, "unix", SocketPath())
	if err != nil {
		return resp, &CommandError{
			Code:    CodeUnavailable,
			Message: fmt.Sprintf("pictures-sync daemon control socket is unavailable: %v", err),
		}
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := requestCtx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resp, &CommandError{Code: CodeUnavailable, Message: fmt.Sprintf("send daemon command: %v", err)}
	}

	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resp, &CommandError{Code: CodeUnavailable, Message: fmt.Sprintf("read daemon response: %v", err)}
	}

	if resp.Status != "ok" {
		return resp, &CommandError{Code: resp.Code, Message: resp.Message}
	}

	return resp, nil
}
