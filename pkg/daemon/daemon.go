package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/captiveportal"
	"github.com/denysvitali/pictures-sync-s3/pkg/daemon/cardhandler"
	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/deviceinfo"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/ledcontroller"
	"github.com/denysvitali/pictures-sync-s3/pkg/netwatchdog"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdcardbrowser"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/signals"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

const (
	// timeSyncCheckInterval is how often to check if NTP has synchronized the system time.
	// This is used during daemon startup to wait for proper time before allowing sync operations.
	timeSyncCheckInterval = 2 * time.Second

	// dnsCheckInterval is how often to check if DNS resolution is working.
	// This is used during daemon startup to ensure network connectivity before sync operations.
	dnsCheckInterval = 2 * time.Second

	timeSyncMaxWait  = 2 * time.Minute
	dnsCheckAttempts = 60
	dnsLookupTimeout = 5 * time.Second

	// mountFailureCheckInterval governs how often the daemon polls for the
	// "device present but unmounted" condition. sdmonitor swallows mount errors
	// silently, so the daemon translates the missing-mount signal into a
	// StatusError so the LED controller surfaces it via PatternError.
	mountFailureCheckInterval = 10 * time.Second
)

// Service represents the main photo backup daemon
type Service struct {
	eventMgr      *events.Manager
	stateMgr      *state.Manager
	settings      *settings.Settings
	syncMgr       *syncmanager.Manager
	ledCtrl       *ledcontroller.Controller
	monitor       *sdmonitor.Monitor
	cardHandler   *cardhandler.Handler
	sigHandler    *signals.Handler
	wifiMgr       *wifimanager.Manager
	captivePortal *captiveportal.Authenticator
	timeSynced    bool // Track if time is properly synced
}

// Config holds configuration for the daemon service
type Config struct {
	NTPSyncDelay time.Duration // How long to wait for NTP sync on startup
}

// DefaultConfig returns the default daemon configuration
func DefaultConfig() Config {
	return Config{
		NTPSyncDelay: 5 * time.Second,
	}
}

// New creates and initializes a new daemon service
func New(cfg Config) (*Service, error) {
	timeSynced := waitForTimeSync()
	waitForDNS()

	eventMgr := events.NewManager()

	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}

	appSettings, syncMgr, err := loadSyncDependencies(stateMgr)
	if err != nil {
		return nil, err
	}

	ledCtrl := startLEDController(stateMgr)
	monitor := sdmonitor.NewMonitor(state.MountDir)
	cardHandler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)
	sigHandler := signals.NewHandler()
	wifiMgr := newWiFiManager()
	captivePortal := startCaptivePortal(wifiMgr)

	return &Service{
		eventMgr:      eventMgr,
		stateMgr:      stateMgr,
		settings:      appSettings,
		syncMgr:       syncMgr,
		ledCtrl:       ledCtrl,
		monitor:       monitor,
		cardHandler:   cardHandler,
		sigHandler:    sigHandler,
		wifiMgr:       wifiMgr,
		captivePortal: captivePortal,
		timeSynced:    timeSynced,
	}, nil
}

func waitForTimeSync() bool {
	log.Println("Waiting for time synchronization (gokrazy NTP daemon)...")

	startWait := time.Now()
	for {
		now := time.Now()
		if isSystemTimeSynced(now) {
			log.Printf("Time synchronized successfully. Current time: %s", now)
			return true
		}

		if time.Since(startWait) > timeSyncMaxWait {
			log.Printf("ERROR: Time sync failed after 2 minutes. Current time: %s", now)
			log.Println("CRITICAL: System time is not synchronized! Sync operations will be disabled.")
			log.Println("Please check network connectivity and NTP configuration.")
			return false
		}

		log.Printf("Time not synced yet (current: %s), waiting...", now)
		time.Sleep(timeSyncCheckInterval)
	}
}

func isSystemTimeSynced(now time.Time) bool {
	return now.Year() > 2020
}

func waitForDNS() bool {
	log.Println("Checking DNS availability...")

	resolver := &net.Resolver{}
	for i := 0; i < dnsCheckAttempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
		_, err := resolver.LookupHost(ctx, "google.com")
		cancel()

		if err == nil {
			log.Println("DNS is ready")
			return true
		}
		if i%5 == 0 {
			log.Printf("DNS not ready yet (attempt %d/%d): %v", i+1, dnsCheckAttempts, err)
		}
		time.Sleep(dnsCheckInterval)
	}

	log.Println("Warning: DNS may not be fully available after 2 minutes. Network operations will retry with backoff.")
	return false
}

func loadSyncDependencies(stateMgr *state.Manager) (*settings.Settings, *syncmanager.Manager, error) {
	appSettings, err := settings.Load()
	if err != nil {
		return nil, nil, err
	}
	log.Printf("Loaded settings: remote=%s:%s", appSettings.GetRemoteName(), appSettings.GetRemotePath())

	hasConfig, err := state.EnsureRcloneConfig()
	if err != nil {
		return nil, nil, err
	}
	if !hasConfig {
		log.Println("Warning: rclone not configured yet. Please configure via web UI.")
	}

	syncMgr := syncmanager.NewManager(
		state.GetRcloneConfigPath(),
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)
	syncMgr.SetGooglePhotos(appSettings.GetGooglePhotosEnabled(), appSettings.GetGooglePhotosRemoteName())
	return appSettings, syncMgr, nil
}

func startLEDController(stateMgr *state.Manager) *ledcontroller.Controller {
	ledCtrl, err := ledcontroller.NewController()
	if err != nil {
		log.Printf("Warning: Failed to initialize LED controller: %v", err)
		return nil
	}
	if err := ledCtrl.Start(stateMgr); err != nil {
		log.Printf("Warning: Failed to start LED controller: %v", err)
	}
	log.Println("LED controller started")
	return ledCtrl
}

func newWiFiManager() *wifimanager.Manager {
	wifiMgr, err := wifimanager.NewManager()
	if err != nil {
		log.Printf("Warning: Failed to initialize WiFi manager: %v", err)
		return nil
	}
	return wifiMgr
}

func startCaptivePortal(wifiMgr *wifimanager.Manager) *captiveportal.Authenticator {
	if wifiMgr == nil {
		return nil
	}

	log.Println("Initializing captive portal authenticator...")
	captivePortal := captiveportal.NewAuthenticator(func() (string, error) {
		return wifiMgr.GetCurrentSSID()
	})
	captivePortal.Start()
	log.Println("Captive portal authenticator started")
	return captivePortal
}

// Run starts the daemon and blocks until shutdown signal is received
func (s *Service) Run() error {
	controlCtx, controlCancel := context.WithCancel(context.Background())
	defer controlCancel()

	go func() {
		netwatchdog.New(netwatchdog.DefaultConfig()).Run(controlCtx)
	}()

	go func() {
		if err := daemoncontrol.ServeWithHandlers(controlCtx, daemoncontrol.Handlers{
			ManualSync:      s.handleManualSyncCommand,
			CancelSync:      s.handleCancelSyncCommand,
			Status:          s.handleStatusCommand,
			History:         s.handleHistoryCommand,
			Devices:         s.handleDevicesCommand,
			FormatSDCard:    s.handleFormatSDCardCommand,
			RedetectSDCard:  s.handleRedetectSDCardCommand,
			SDCardFiles:     s.handleSDCardFilesCommand,
			SDCardPreview:   s.handleSDCardPreviewCommand,
			SDCardThumbnail: s.handleSDCardThumbnailCommand,
			Subscribe:       s.handleSubscribeCommand,
		}); err != nil {
			log.Printf("Daemon control server stopped: %v", err)
		}
	}()

	// Get event channel BEFORE starting monitor to avoid missing events
	eventChan := s.monitor.Events()
	log.Printf("Event channel obtained: %p", eventChan)

	// Now start the monitor - any events sent during Start() will be buffered
	if err := s.monitor.Start(); err != nil {
		return err
	}
	log.Println("SD card monitor started")

	// Set initial status
	log.Println("Setting initial status to idle...")
	if !s.monitor.IsCardMounted() {
		if err := s.stateMgr.SetSDCard(false, ""); err != nil {
			log.Printf("Warning: Failed to clear stale SD card state: %v", err)
		}
	}
	s.stateMgr.SetStatus(state.StatusIdle)

	// Note: No need to manually check for already-mounted cards here.
	// The SD monitor's Start() method calls checkDevices() which will detect
	// any already-mounted cards and send an insertion event automatically.
	// This prevents duplicate events and race conditions.

	log.Println("Ready - waiting for SD card insertion...")
	log.Println("Entering main event loop...")

	mountWatchTicker := time.NewTicker(mountFailureCheckInterval)
	defer mountWatchTicker.Stop()
	mountFailureActive := false

	// Main event loop
	for {
		select {
		case <-s.sigHandler.Channel():
			log.Println("Received shutdown signal, exiting...")
			controlCancel()
			return nil

		case event := <-eventChan:
			s.handleMonitorEvent(event, &mountFailureActive)

		case <-mountWatchTicker.C:
			s.checkSilentMountFailure(&mountFailureActive)
		}
	}
}

func (s *Service) handleMonitorEvent(event sdmonitor.Event, mountFailureActive *bool) {
	log.Printf("Received event from monitor: %+v", event)

	switch event.Type {
	case sdmonitor.EventInserted:
		log.Printf("Processing card insertion event: %s at %s", event.DevName, event.MountPath)

		// A successful insertion implies the mount succeeded; clear any
		// previous silent-mount-failure flag so the next idle/syncing
		// status transition can clear the error LED.
		*mountFailureActive = false

		if !s.timeSynced {
			log.Println("ERROR: Cannot sync - system time is not synchronized!")
			log.Println("Card detected but sync disabled due to incorrect system time")
			s.stateMgr.SetStatus(state.StatusError)
			s.stateMgr.SetError("System time not synchronized - sync disabled")
			return
		}

		s.cardHandler.HandleInserted(event)
	case sdmonitor.EventRemoved:
		log.Printf("Processing card removal event: %s", event.DevName)
		*mountFailureActive = false
		s.cardHandler.HandleRemoved(event)
	}
}

// checkSilentMountFailure polls for storage devices that look like SD cards
// but never reached the monitor's mounted state. sdmonitor logs the mount
// error and returns, so without this watcher the user sees no feedback at
// all. When the condition is detected we promote it to StatusError so the
// LED controller drives PatternError; the next successful sync or idle
// transition reverts the LED.
func (s *Service) checkSilentMountFailure(active *bool) {
	if s.monitor.IsCardMounted() {
		if *active {
			*active = false
		}
		return
	}

	devices, err := sdmonitor.ListAllStorageDevices()
	if err != nil {
		log.Printf("mount-failure watcher: failed to list devices: %v", err)
		return
	}
	if len(devices) == 0 {
		if *active {
			*active = false
		}
		return
	}

	// If the running sync manager is busy we already have a sync-side error
	// signal; do not double-report.
	if s.syncMgr.IsRunning() {
		return
	}

	if *active {
		return
	}

	devNames := make([]string, 0, len(devices))
	for _, d := range devices {
		devNames = append(devNames, d.DevicePath)
	}
	log.Printf("mount-failure watcher: %d storage device(s) present but none mounted (%s); surfacing as error",
		len(devices), strings.Join(devNames, ", "))

	if err := s.stateMgr.SetError("SD card detected but mount failed"); err != nil {
		log.Printf("mount-failure watcher: failed to set error message: %v", err)
	}
	if err := s.stateMgr.SetStatus(state.StatusError); err != nil {
		log.Printf("mount-failure watcher: failed to set error status: %v", err)
	}
	*active = true
}

func (s *Service) handleManualSyncCommand(ctx context.Context, devicePath string) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}

	if !s.timeSynced {
		log.Println("ERROR: Cannot start manual sync - system time is not synchronized!")
		s.stateMgr.SetStatus(state.StatusError)
		s.stateMgr.SetError("System time not synchronized - sync disabled")
		return daemoncontrol.Error(daemoncontrol.CodeInternalError, "System time not synchronized - sync disabled")
	}

	if err := s.cardHandler.HandleManualSync(devicePath); err != nil {
		log.Printf("Manual sync command rejected: %v", err)
		switch err.Error() {
		case "no SD card mounted":
			return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, err.Error())
		case "selected device is not mounted":
			return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, err.Error())
		case "sync already in progress or starting":
			return daemoncontrol.Error(daemoncontrol.CodeSyncAlreadyActive, err.Error())
		default:
			return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
		}
	}

	return daemoncontrol.OK("Sync start requested")
}

func (s *Service) handleCancelSyncCommand(ctx context.Context) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}

	if !s.syncMgr.IsRunning() {
		return daemoncontrol.Error(daemoncontrol.CodeSyncAlreadyActive, "no sync in progress")
	}

	log.Println("Manual sync cancellation requested via daemon control")
	if err := s.syncMgr.Cancel(); err != nil {
		log.Printf("Manual sync cancellation failed: %v", err)
		return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
	}

	// Surface the cancellation to the UI immediately, but leave CurrentSync
	// intact. The sync goroutine in cardhandler.performSync owns the final
	// state transition and will move us to Idle (with a "cancelled by user"
	// history record) once rclone has actually unwound.
	if err := s.stateMgr.SetStatus(state.StatusCancelling); err != nil {
		log.Printf("Failed to set cancelling status: %v", err)
	}

	return daemoncontrol.OK("Sync cancelled")
}

func (s *Service) handleStatusCommand(ctx context.Context) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}

	return daemoncontrol.OKData("status", s.stateMgr.GetState())
}

func (s *Service) handleHistoryCommand(ctx context.Context) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}

	return daemoncontrol.OKData("history", s.stateMgr.GetHistory())
}

func (s *Service) handleDevicesCommand(ctx context.Context) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}

	devices, err := sdmonitor.ListAllStorageDevices()
	if err != nil {
		return daemoncontrol.Error(daemoncontrol.CodeInternalError, fmt.Sprintf("failed to list devices: %v", err))
	}

	if err := s.stateMgr.SetAvailableDevices(deviceinfo.ToStateDevices(devices)); err != nil {
		log.Printf("Warning: Failed to update daemon device state: %v", err)
	}

	return daemoncontrol.OKData("devices", devices)
}

func (s *Service) handleFormatSDCardCommand(ctx context.Context, devicePath, label string) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}
	if s.syncMgr.IsRunning() {
		return daemoncontrol.Error(daemoncontrol.CodeSyncAlreadyActive, "cannot format SD card while sync is in progress")
	}
	if devicePath == "" {
		return daemoncontrol.Error(daemoncontrol.CodeInvalidDevice, "device_path is required")
	}
	if !sdmonitor.IsSupportedDevicePath(devicePath) {
		return daemoncontrol.Error(daemoncontrol.CodeInvalidDevice, "unsupported SD card device path")
	}
	if err := sdmonitor.ValidateVolumeLabel(strings.TrimSpace(label)); err != nil {
		return daemoncontrol.Error(daemoncontrol.CodeInvalidDevice, err.Error())
	}

	log.Printf("Manual SD card format requested for %s", devicePath)
	if err := s.monitor.FormatCurrentDevice(ctx, devicePath, label); err != nil {
		log.Printf("SD card format failed: %v", err)
		if !s.monitor.IsCardMounted() {
			if stateErr := s.stateMgr.SetSDCard(false, ""); stateErr != nil {
				log.Printf("Warning: Failed to clear SD card state after format failure: %v", stateErr)
			}
		}
		if stateErr := s.stateMgr.SetError(err.Error()); stateErr != nil {
			log.Printf("Warning: Failed to record SD card format error in state: %v", stateErr)
		}
		switch err.Error() {
		case "no SD card mounted":
			return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, err.Error())
		case "selected device is not mounted":
			return daemoncontrol.Error(daemoncontrol.CodeInvalidDevice, err.Error())
		default:
			return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
		}
	}

	if err := s.stateMgr.SetSDCard(false, ""); err != nil {
		log.Printf("Warning: Failed to clear SD card state after format: %v", err)
	}
	s.stateMgr.SetStatus(state.StatusIdle)

	return daemoncontrol.OK("SD card formatted")
}

func (s *Service) handleRedetectSDCardCommand(ctx context.Context) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}
	if s.syncMgr.IsRunning() {
		return daemoncontrol.Error(daemoncontrol.CodeSyncAlreadyActive, "cannot re-detect SD card while sync is in progress")
	}

	log.Println("Manual SD card re-detect requested")
	if err := s.monitor.RedetectCurrentDevice(); err != nil {
		log.Printf("SD card re-detect failed: %v", err)
		if stateErr := s.stateMgr.SetSDCard(false, ""); stateErr != nil {
			log.Printf("Warning: Failed to clear SD card state after re-detect failure: %v", stateErr)
		}
		switch err.Error() {
		case "no SD card detected":
			return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, err.Error())
		default:
			return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
		}
	}

	if err := s.stateMgr.SetSDCard(true, s.monitor.GetMountPath()); err != nil {
		log.Printf("Warning: Failed to update SD card state after re-detect: %v", err)
	}
	if err := s.stateMgr.SetSDCardDevice(s.monitor.GetCurrentDevice()); err != nil {
		log.Printf("Warning: Failed to update SD card device after re-detect: %v", err)
	}
	if err := s.stateMgr.SetStatus(state.StatusDetected); err != nil {
		log.Printf("Warning: Failed to update status after re-detect: %v", err)
	}

	return daemoncontrol.OK("SD card re-detected")
}

func (s *Service) handleSDCardFilesCommand(ctx context.Context, requestedPath string) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}
	if !s.monitor.IsCardMounted() {
		return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, "no SD card mounted")
	}

	result, err := sdcardbrowser.ListFiles(s.monitor.GetMountPath(), requestedPath)
	if err != nil {
		return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
	}

	return daemoncontrol.OKData("sdcard files", result)
}

func (s *Service) handleSDCardPreviewCommand(ctx context.Context, requestedPath string) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}
	if !s.monitor.IsCardMounted() {
		return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, "no SD card mounted")
	}

	result, err := sdcardbrowser.ReadPreview(s.monitor.GetMountPath(), requestedPath)
	if err != nil {
		return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
	}

	return daemoncontrol.OKData("sdcard preview", result)
}

func (s *Service) handleSDCardThumbnailCommand(ctx context.Context, requestedPath string) daemoncontrol.Response {
	if ctx.Err() != nil {
		return daemoncontrol.Error(daemoncontrol.CodeUnavailable, "pictures-sync daemon is shutting down")
	}
	if !s.monitor.IsCardMounted() {
		return daemoncontrol.Error(daemoncontrol.CodeNoSDCardMounted, "no SD card mounted")
	}

	result, err := sdcardbrowser.ReadThumbnail(s.monitor.GetMountPath(), requestedPath)
	if err != nil {
		return daemoncontrol.Error(daemoncontrol.CodeInternalError, err.Error())
	}

	return daemoncontrol.OKData("sdcard thumbnail", result)
}

// handleSubscribeCommand implements the streaming subscribe handler. It
// pushes the current state snapshot to the client immediately on connect,
// then forwards every subsequent state update and event until ctx is
// cancelled (client disconnect, daemon shutdown, etc.). The control-socket
// dispatcher serialises envelopes to the wire; this goroutine just needs to
// keep producing them.
func (s *Service) handleSubscribeCommand(ctx context.Context, out chan<- daemoncontrol.Envelope) {
	stateUpdates := s.stateMgr.Subscribe()
	eventUpdates := s.eventMgr.Subscribe()
	defer s.stateMgr.Unsubscribe(stateUpdates)
	defer s.eventMgr.Unsubscribe(eventUpdates)

	// Initial snapshot so the client immediately has a coherent view without
	// waiting for the next mutation.
	initial := s.stateMgr.GetState()
	if !sendEnvelope(ctx, out, daemoncontrol.Envelope{
		Kind:  daemoncontrol.EnvelopeKindState,
		State: &initial,
	}) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case st, ok := <-stateUpdates:
			if !ok {
				return
			}
			snap := st
			if !sendEnvelope(ctx, out, daemoncontrol.Envelope{
				Kind:  daemoncontrol.EnvelopeKindState,
				State: &snap,
			}) {
				return
			}
		case ev, ok := <-eventUpdates:
			if !ok {
				return
			}
			snap := ev
			if !sendEnvelope(ctx, out, daemoncontrol.Envelope{
				Kind:  daemoncontrol.EnvelopeKindEvent,
				Event: &snap,
			}) {
				return
			}
		}
	}
}

// sendEnvelope writes a single envelope to out, respecting ctx cancellation.
// Returns false if ctx is done so the caller can unwind cleanly.
func sendEnvelope(ctx context.Context, out chan<- daemoncontrol.Envelope, env daemoncontrol.Envelope) bool {
	select {
	case out <- env:
		return true
	case <-ctx.Done():
		return false
	}
}

// Shutdown gracefully shuts down the daemon
func (s *Service) Shutdown() {
	log.Println("Shutting down daemon...")

	if s.captivePortal != nil {
		s.captivePortal.Stop()
	}

	if s.monitor != nil {
		s.monitor.Stop()
	}

	if s.ledCtrl != nil {
		s.ledCtrl.Stop()
	}

	if s.sigHandler != nil {
		s.sigHandler.Stop()
	}

	if s.stateMgr != nil {
		s.stateMgr.Close()
	}

	log.Println("Daemon shutdown complete")
}
