package provisionap

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProcessRunner starts external processes. It is injectable for tests.
type ProcessRunner interface {
	Start(ctx context.Context, name string, args ...string) (Process, error)
}

// Process is a running external process.
type Process interface {
	Wait() error
	Stop() error
}

type execRunner struct{}

func (execRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd}, nil
}

type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) Wait() error {
	return p.cmd.Wait()
}

func (p *execProcess) Stop() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(os.Interrupt)
}

func writeHostapdConfig(cfg Config) (string, error) {
	if err := os.MkdirAll(cfg.ConfigDir, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(cfg.ConfigDir, "hostapd.conf")
	if err := os.WriteFile(path, []byte(RenderHostapdConfig(cfg)), 0600); err != nil {
		return "", err
	}
	return path, nil
}

// RenderHostapdConfig renders the hostapd configuration for provisioning mode.
func RenderHostapdConfig(cfg Config) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "interface=%s\n", cfg.Interface)
	b.WriteString("driver=nl80211\n")
	fmt.Fprintf(&b, "ssid=%s\n", escapeHostapdValue(cfg.SSID))
	b.WriteString("hw_mode=g\n")
	b.WriteString("channel=6\n")
	b.WriteString("ieee80211n=1\n")
	b.WriteString("wmm_enabled=1\n")
	b.WriteString("auth_algs=1\n")
	b.WriteString("wpa=2\n")
	fmt.Fprintf(&b, "wpa_passphrase=%s\n", escapeHostapdValue(cfg.PSK))
	b.WriteString("wpa_key_mgmt=WPA-PSK\n")
	b.WriteString("rsn_pairwise=CCMP\n")
	b.WriteString("ignore_broadcast_ssid=0\n")
	return b.String()
}

func escapeHostapdValue(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\n", "", "\r", "")
	return replacer.Replace(value)
}
