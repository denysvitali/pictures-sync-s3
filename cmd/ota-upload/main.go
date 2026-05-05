package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gokrazy/updater"
	"github.com/spf13/cobra"

	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "ota-upload: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	targetURL string
	imagePath string
	timeout   time.Duration
	testboot  bool
	noReboot  bool
	noKexec   bool
	insecure  bool
}

func newRootCommand() *cobra.Command {
	opts := options{
		targetURL: envDefault("OTA_GOKRAZY_UPDATE_URL", ota.DefaultUpdateURL),
		timeout:   30 * time.Minute,
		insecure:  envBool(ota.UpdateInsecureEnv, false),
	}

	cmd := &cobra.Command{
		// #nosec G101 -- example placeholder password in usage string
		Use:   "ota-upload --image photo-backup-rpi4b-root.squashfs.gz --target https://gokrazy:password@device/",
		Short: "Upload a gzipped gokrazy root image to a running device",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.targetURL, "target", opts.targetURL, "gokrazy updater base URL, including basic auth if needed")
	flags.StringVar(&opts.imagePath, "image", opts.imagePath, "path to .squashfs.gz OTA root image")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "overall update timeout")
	flags.BoolVar(&opts.testboot, "testboot", opts.testboot, "mark the new root for a test boot instead of switching permanently")
	flags.BoolVar(&opts.noReboot, "no-reboot", opts.noReboot, "upload and switch/testboot without rebooting")
	flags.BoolVar(&opts.noKexec, "no-kexec", opts.noKexec, "request a full reboot instead of kexec")
	flags.BoolVar(&opts.insecure, "insecure", opts.insecure, "skip TLS certificate verification")

	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func run(ctx context.Context, opts options) error {
	file, err := os.Open(opts.imagePath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip image: %w", err)
	}
	defer gz.Close()

	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	baseURL := ensureTrailingSlash(strings.TrimSpace(opts.targetURL))
	client := ota.NewUpdateHTTPClient(baseURL, opts.timeout, opts.insecure)
	target, err := updater.NewTarget(ctx, baseURL, client)
	if err != nil {
		return fmt.Errorf("connect to gokrazy updater: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Uploading %s to %supdate/root...\n", opts.imagePath, baseURL)
	progress := newProgressReader(gz)
	stopProgress := progress.report(ctx, os.Stderr, 2*time.Second)
	if err := target.StreamTo(ctx, "root", progress); err != nil {
		stopProgress()
		if err == io.ErrUnexpectedEOF {
			return fmt.Errorf("stream root image: truncated gzip input: %w", err)
		}
		return fmt.Errorf("stream root image: %w", err)
	}
	stopProgress()

	if opts.testboot {
		fmt.Fprintln(os.Stderr, "Marking new root for test boot...")
		if err := target.Testboot(ctx); err != nil {
			return fmt.Errorf("testboot: %w", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Switching to new root partition...")
		if err := target.Switch(ctx); err != nil {
			return fmt.Errorf("switch root partition: %w", err)
		}
	}

	if opts.noReboot {
		fmt.Fprintln(os.Stderr, "Update installed. Reboot skipped.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "Requesting reboot...")
	rebootOpts := []updater.RebootOption{}
	if opts.noKexec {
		rebootOpts = append(rebootOpts, updater.WithKexec(false))
	}
	if err := target.Reboot(ctx, rebootOpts...); err != nil {
		return fmt.Errorf("reboot: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Update installed; reboot requested.")
	return nil
}

func ensureTrailingSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

type progressReader struct {
	reader io.Reader
	bytes  atomic.Int64
}

func newProgressReader(reader io.Reader) *progressReader {
	return &progressReader{reader: reader}
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.bytes.Add(int64(n))
	}
	return n, err
}

func (r *progressReader) report(ctx context.Context, out io.Writer, interval time.Duration) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	start := time.Now()

	print := func(final bool) {
		transferred := r.bytes.Load()
		elapsed := time.Since(start)
		speed := 0.0
		if elapsed > 0 {
			speed = float64(transferred) / elapsed.Seconds()
		}

		if final {
			fmt.Fprintf(out, "\rUploaded %s in %s (%s)\n", utils.FormatBytes(transferred), elapsed.Round(time.Second), utils.FormatSpeed(speed))
			return
		}
		fmt.Fprintf(out, "\rUploaded %s (%s)", utils.FormatBytes(transferred), utils.FormatSpeed(speed))
	}

	go func() {
		defer close(stopped)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		print(false)
		for {
			select {
			case <-ctx.Done():
				print(true)
				return
			case <-done:
				print(true)
				return
			case <-ticker.C:
				print(false)
			}
		}
	}()

	var stoppedOnce atomic.Bool
	return func() {
		if stoppedOnce.CompareAndSwap(false, true) {
			close(done)
			<-stopped
		}
	}
}
