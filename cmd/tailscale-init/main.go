package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
)

const (
	defaultHostname  = "photo-backup"
	defaultUpArgSSH  = "--ssh"
	defaultAcceptDNS = "--accept-dns=false"
	tailscaleBinary  = "/user/tailscale"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	authKeyPath := getenv("TS_AUTH_KEY_PATH", settings.TailscaleAuthKeyFile)
	hostname := getenv("TS_HOSTNAME", defaultHostname)
	extraArgs := tailscaleUpArgs(os.Getenv("TS_TAILSCALE_UP_ARGS"))

	key, keyPath, err := readAuthKey(candidateAuthKeyPaths(authKeyPath))
	if err != nil {
		log.Printf("tailscale-init: no readable auth key found, skipping Tailscale connect: %v", err)
		return
	}

	if key == "" {
		log.Printf("tailscale-init: auth key file %q is empty, skipping Tailscale connect", keyPath)
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

func tailscaleUpArgs(configured string) []string {
	args := strings.Fields(configured)
	if len(args) == 0 {
		args = append(args, defaultUpArgSSH)
	}
	if !hasFlag(args, "--accept-dns") {
		args = append(args, defaultAcceptDNS)
	}
	return args
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func candidateAuthKeyPaths(configured string) []string {
	paths := []string{
		configured,
		settings.TailscaleAuthKeyFile,
		settings.LegacyTailscaleAuthKeyFile,
	}
	seen := make(map[string]bool, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result
}

func readAuthKey(paths []string) (string, string, error) {
	var failures []string
	var emptyPaths []string
	for _, path := range paths {
		// #nosec G304 -- paths are controlled Tailscale auth-key locations.
		authKey, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		key := strings.TrimSpace(string(authKey))
		if key == "" {
			emptyPaths = append(emptyPaths, path)
			continue
		}
		return key, path, nil
	}
	if len(emptyPaths) > 0 {
		return "", emptyPaths[0], nil
	}
	return "", "", fmt.Errorf("%s", strings.Join(failures, "; "))
}
