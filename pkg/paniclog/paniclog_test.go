package paniclog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestReadStoredReturnsRawCrashOutput(t *testing.T) {
	dir := t.TempDir()
	crashPath := filepath.Join(dir, "webui-panic.log")

	if err := os.WriteFile(crashPath, []byte("panic: background failed\n\ngoroutine 7 [running]:\n"), 0600); err != nil {
		t.Fatalf("write crash output: %v", err)
	}

	record, err := ReadStored(filepath.Join(dir, "missing.json"), crashPath)
	if err != nil {
		t.Fatalf("ReadStored() error = %v", err)
	}
	if record == nil {
		t.Fatal("ReadStored() = nil, want record")
	}
	if record.Source != "webui-crash" {
		t.Fatalf("Source = %q, want webui-crash", record.Source)
	}
	if record.Message != "panic: background failed" {
		t.Fatalf("Message = %q, want panic line", record.Message)
	}
	if !record.Raw {
		t.Fatal("Raw = false, want true")
	}
}

func TestConfigureCrashOutputCapturesUnhandledGoroutinePanic(t *testing.T) {
	if os.Getenv("PANICLOG_CRASH_HELPER") == "1" {
		path := os.Getenv("PANICLOG_CRASH_PATH")
		if err := ConfigureCrashOutput(path); err != nil {
			t.Fatalf("ConfigureCrashOutput() error = %v", err)
		}
		go func() {
			panic("general panic capture")
		}()
		time.Sleep(time.Second)
		return
	}

	path := filepath.Join(t.TempDir(), "webui-panic.log")
	cmd := exec.Command(os.Args[0], "-test.run=TestConfigureCrashOutputCapturesUnhandledGoroutinePanic")
	cmd.Env = append(os.Environ(),
		"PANICLOG_CRASH_HELPER=1",
		"PANICLOG_CRASH_PATH="+path,
	)

	err := cmd.Run()
	if err == nil {
		t.Fatal("helper exited successfully, want panic failure")
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read crash output: %v", readErr)
	}
	if !strings.Contains(string(data), "panic: general panic capture") {
		t.Fatalf("crash output missing panic: %s", string(data))
	}
}
