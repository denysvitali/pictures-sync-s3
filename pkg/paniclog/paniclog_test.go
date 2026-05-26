package paniclog

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureReadAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webui-panic.json")

	if err := Capture(path, "test-source", "boom"); err != nil {
		t.Fatalf("Capture() error = %v", err)
	}

	record, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if record == nil {
		t.Fatal("Read() record = nil, want record")
	}
	if record.Source != "test-source" {
		t.Fatalf("Source = %q, want test-source", record.Source)
	}
	if record.Message != "boom" {
		t.Fatalf("Message = %q, want boom", record.Message)
	}
	if !strings.Contains(record.Stack, "TestCaptureReadAndClear") {
		t.Fatalf("Stack does not include test function: %q", record.Stack)
	}

	if err := Clear(path); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	record, err = Read(path)
	if err != nil {
		t.Fatalf("Read() after clear error = %v", err)
	}
	if record != nil {
		t.Fatalf("Read() after clear = %#v, want nil", record)
	}
}
