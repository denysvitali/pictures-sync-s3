package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultAuthKeyPath = "/perm/tailscale/authkey"
	defaultHostname    = "photo-backup"
	tailscaleBinary    = "/user/tailscale"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	authKeyPath := getenv("TS_AUTH_KEY_PATH", defaultAuthKeyPath)
	hostname := getenv("TS_HOSTNAME", defaultHostname)
	extraArgs := strings.Fields(os.Getenv("TS_TAILSCALE_UP_ARGS"))

	// #nosec G304 -- authKeyPath is a controlled config path (/perm/tailscale/authkey)
	authKey, err := os.ReadFile(authKeyPath)
	if err != nil {
		log.Printf("tailscale-init: auth key path %q not readable, skipping Tailscale connect: %v", authKeyPath, err)
		return
	}

	key := strings.TrimSpace(string(authKey))
	if key == "" {
		log.Printf("tailscale-init: auth key file %q is empty, skipping Tailscale connect", authKeyPath)
		return
	}

	args := []string{"up", "--auth-key=" + key, "--hostname=" + hostname}
	args = append(args, extraArgs...)

	log.Printf("tailscale-init: running /user/tailscale up")
	// #nosec G204 G702 -- tailscaleBinary is a const, args from trusted config/env on embedded device
	cmd := exec.Command(tailscaleBinary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("tailscale-init: tailscale up failed: %v", err)
	}
}

func getenv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
