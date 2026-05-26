package paniclog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"sort"
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

type Store struct {
	Panics []Record `json:"panics"`
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
	return Append(path, record)
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

func Append(path string, record Record) error {
	if path == "" {
		path = DefaultPath
	}
	records, err := ReadAll(path)
	if err != nil {
		return err
	}
	records = append(records, record)
	return writeAll(path, records)
}

func writeAll(path string, records []Record) error {
	if path == "" {
		path = DefaultPath
	}
	data, err := json.MarshalIndent(Store{Panics: records}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal panic records: %w", err)
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
	records, err := ReadAll(path)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[len(records)-1], nil
}

func ReadStored(recordPath, crashPath string) (*Record, error) {
	records, err := ReadAllStored(recordPath, crashPath)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func ReadAll(path string) ([]Record, error) {
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

	var store Store
	if err := json.Unmarshal(data, &store); err == nil && store.Panics != nil {
		return store.Panics, nil
	}

	var records []Record
	if err := json.Unmarshal(data, &records); err == nil {
		return records, nil
	}

	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("parse panic record: %w", err)
	}
	return []Record{record}, nil
}

func ReadAllStored(recordPath, crashPath string) ([]Record, error) {
	records, recordErr := ReadAll(recordPath)
	crashes, crashErr := readCrashRecords(crashPath)
	if recordErr != nil {
		return nil, recordErr
	}
	if crashErr != nil {
		return nil, crashErr
	}

	all := append([]Record{}, records...)
	all = append(all, crashes...)
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Time > all[j].Time
	})
	return all, nil
}

func readCrashRecords(path string) ([]Record, error) {
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
	return crashRecords(string(data), info.ModTime().UTC().Format(time.RFC3339Nano)), nil
}

func crashRecords(raw, timestamp string) []Record {
	chunks := splitCrashLog(raw)
	records := make([]Record, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		records = append(records, Record{
			Time:    timestamp,
			Source:  "webui-crash",
			Message: crashMessage(chunk),
			Stack:   chunk,
			Raw:     true,
		})
	}
	return records
}

func splitCrashLog(raw string) []string {
	lines := strings.Split(raw, "\n")
	var chunks []string
	var current []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		startsCrash := strings.HasPrefix(trimmed, "panic: ") || strings.HasPrefix(trimmed, "fatal error: ")
		if startsCrash && len(current) > 0 {
			chunks = append(chunks, strings.Join(current, "\n"))
			current = nil
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, "\n"))
	}
	return chunks
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
