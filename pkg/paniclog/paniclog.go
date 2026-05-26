package paniclog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const DefaultPath = "/perm/webui-panic.json"
const DefaultCrashPath = "/perm/webui-panic.log"

type Record struct {
	Time    string `json:"time"`
	Source  string `json:"source"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
	Raw     bool   `json:"raw,omitempty"`
}

func Capture(path, source string, recovered any) error {
	if path == "" {
		path = DefaultPath
	}
	record := Record{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:  source,
		Message: fmt.Sprint(recovered),
		Stack:   string(debug.Stack()),
	}
	return Write(path, record)
}

func Write(path string, record Record) error {
	if path == "" {
		path = DefaultPath
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal panic record: %w", err)
	}
	data = append(data, '\n')
	return utils.AtomicWrite(path, data, 0600)
}

func ConfigureCrashOutput(path string) error {
	if path == "" {
		path = DefaultCrashPath
	}
	// #nosec G304 -- path is a controlled application path.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open crash output: %w", err)
	}
	defer f.Close()

	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		return fmt.Errorf("configure crash output: %w", err)
	}
	return nil
}

func Read(path string) (*Record, error) {
	return readJSON(path)
}

func ReadStored(recordPath, crashPath string) (*Record, error) {
	record, recordErr := readJSON(recordPath)
	crash, crashErr := readCrash(crashPath)
	if recordErr != nil {
		return nil, recordErr
	}
	if crashErr != nil {
		return nil, crashErr
	}
	if record == nil {
		return crash, nil
	}
	if crash == nil {
		return record, nil
	}
	if crash.Time > record.Time {
		return crash, nil
	}
	return record, nil
}

func readJSON(path string) (*Record, error) {
	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read panic record: %w", err)
	}

	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parse panic record: %w", err)
	}
	return &record, nil
}

func readCrash(path string) (*Record, error) {
	if path == "" {
		path = DefaultCrashPath
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat crash output: %w", err)
	}
	if info.Size() == 0 {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read crash output: %w", err)
	}
	raw := string(data)
	return &Record{
		Time:    info.ModTime().UTC().Format(time.RFC3339Nano),
		Source:  "webui-crash",
		Message: crashMessage(raw),
		Stack:   raw,
		Raw:     true,
	}, nil
}

func crashMessage(raw string) string {
	fallback := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if fallback == "" {
			fallback = line
		}
		if strings.HasPrefix(line, "panic: ") || strings.HasPrefix(line, "fatal error: ") {
			return line
		}
	}
	if fallback != "" {
		return fallback
	}
	return "unhandled process crash"
}

func Clear(path string) error {
	if path == "" {
		path = DefaultPath
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear panic record: %w", err)
	}
	return nil
}

func ClearStored(recordPath, crashPath string) error {
	if recordPath == "" {
		recordPath = DefaultPath
	}
	if crashPath == "" {
		crashPath = DefaultCrashPath
	}
	for _, path := range []string{recordPath, crashPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clear panic record: %w", err)
		}
	}
	return nil
}
