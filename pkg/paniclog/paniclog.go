package paniclog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const DefaultPath = "/perm/webui-panic.json"

type Record struct {
	Time    string `json:"time"`
	Source  string `json:"source"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
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

func Read(path string) (*Record, error) {
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

func Clear(path string) error {
	if path == "" {
		path = DefaultPath
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear panic record: %w", err)
	}
	return nil
}
