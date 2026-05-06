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
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/ledcontroller"
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
	// Wait for gokrazy's NTP daemon to sync time
	log.Println("Waiting for time synchronization (gokrazy NTP daemon)...")

	// Track whether time sync succeeded
	var timeSynced bool

	// Wait until we have a reasonable time (not 1970 epoch)
	startWait := time.Now()
	for {
		now := time.Now()
		// Check if time is after year 2020 (definitely synced)
		if now.Year() > 2020 {
			log.Printf("Time synchronized successfully. Current time: %s", now)
			timeSynced = true
			break
		}

		// Check if we've been waiting too long (fallback after 2 minutes)
		if time.Since(startWait) > 2*time.Minute {
			log.Printf("ERROR: Time sync failed after 2 minutes. Current time: %s", now)
			log.Println("CRITICAL: System time is not synchronized! Sync operations will be disabled.")
			log.Println("Please check network connectivity and NTP configuration.")
			timeSynced = false
			break
		}

		log.Printf("Time not synced yet (current: %s), waiting...", now)
		time.Sleep(timeSyncCheckInterval)
	}

	// Wait for DNS to be available (check if we can resolve a common domain)
	log.Println("Checking DNS availability...")
	dnsReady := false
	for i := 0; i < 60; i++ { // Try for 2 minutes (60 * 2s)
		// Create context with 5-second timeout for DNS lookup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		// Use custom resolver with context for timeout protection
		resolver := &net.Resolver{}
		_, err := resolver.LookupHost(ctx, "google.com")
		cancel() // Always cancel context to free resources

		if err == nil {
			log.Println("DNS is ready")
			dnsReady = true
			break
		}
		// Only log every 5 attempts to reduce log spam
		if i%5 == 0 {
			log.Printf("DNS not ready yet (attempt %d/60): %v", i+1, err)
		}
		time.Sleep(dnsCheckInterval)
	}

	if !dnsReady {
		log.Println("Warning: DNS may not be fully available after 2 minutes. Network operations will retry with backoff.")
	}

	// Initialize event manager
	eventMgr := events.NewManager()

	// Initialize state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}

	// Load settings
	appSettings, err := settings.Load()
	if err != nil {
		return nil, err
	}
	log.Printf("Loaded settings: remote=%s:%s", appSettings.GetRemoteName(), appSettings.GetRemotePath())

	// Check if rclone is configured
	hasConfig, err := state.EnsureRcloneConfig()
	if err != nil {
		return nil, err
	}
	if !hasConfig {
		log.Println("Warning: rclone not configured yet. Please configure via web UI.")
	}

	// Initialize sync manager
	syncMgr := syncmanager.NewManager(
		state.GetRcloneConfigPath(),
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)
	// Update Google Photos settings
	syncMgr.SetGooglePhotos(appSettings.GetGooglePhotosEnabled(), appSettings.GetGooglePhotosRemoteName())

	// Initialize LED controller
	ledCtrl, err := ledcontroller.NewController()
	if err != nil {
		log.Printf("Warning: Failed to initialize LED controller: %v", err)
	} else {
		if err := ledCtrl.Start(stateMgr); err != nil {
			log.Printf("Warning: Failed to start LED controller: %v", err)
		}
		log.Println("LED controller started")
	}

	// Initialize SD card monitor
	monitor := sdmonitor.NewMonitor(state.MountDir)

	// Initialize card handler
	cardHandler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	// Initialize signal handler
	sigHandler := signals.NewHandler()

	// Initialize WiFi manager for captive portal
	wifiMgr, err := wifimanager.NewManager()
	if err != nil {
		log.Printf("Warning: Failed to initialize WiFi manager: %v", err)
	}

	// Initialize captive portal authenticator
	var captivePortal *captiveportal.Authenticator
	if wifiMgr != nil {
		log.Println("Initializing captive portal authenticator...")
		captivePortal = captiveportal.NewAuthenticator(func() (string, error) {
			return wifiMgr.GetCurrentSSID()
		})
		captivePortal.Start()
		log.Println("Captive portal authenticator started")
	}

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

// Run starts the daemon and blocks until shutdown signal is received
func (s *Service) Run() error {
	controlCtx, controlCancel := context.WithCancel(context.Background())
	defer controlCancel()

	go func() {
		if err := daemoncontrol.ServeWithHandlers(controlCtx, daemoncontrol.Handlers{
			ManualSync:      s.handleManualSyncCommand,
			CancelSync:      s.handleCancelSyncCommand,
			Status:          s.handleStatusCommand,
			History:         s.handleHistoryCommand,
			Devices:         s.handleDevicesCommand,
			FormatSDCard:    s.handleFormatSDCardCommand,
			SDCardFiles:     s.handleSDCardFilesCommand,
			SDCardPreview:   s.handleSDCardPreviewCommand,
			SDCardThumbnail: s.handleSDCardThumbnailCommand,
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

	// Main event loop
	for {
		select {
		case <-s.sigHandler.Channel():
			log.Println("Received shutdown signal, exiting...")
			controlCancel()
			return nil

		case event := <-eventChan:
			log.Printf("Received event from monitor: %+v", event)
			switch event.Type {
			case sdmonitor.EventInserted:
				log.Printf("Processing card insertion event: %s at %s", event.DevName, event.MountPath)

				// Check if time is synced before allowing sync operations
				if !s.timeSynced {
					log.Println("ERROR: Cannot sync - system time is not synchronized!")
					log.Println("Card detected but sync disabled due to incorrect system time")
					s.stateMgr.SetStatus(state.StatusError)
					s.stateMgr.SetError("System time not synchronized - sync disabled")
					break
				}

				s.cardHandler.HandleInserted(event)
			case sdmonitor.EventRemoved:
				log.Printf("Processing card removal event: %s", event.DevName)
				s.cardHandler.HandleRemoved(event)
			}
		}
	}
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

	s.stateMgr.FinishSync(false, fmt.Errorf("cancelled by user"))
	s.stateMgr.SetStatus(state.StatusIdle)

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

	stateDevices := make([]state.DeviceInfo, len(devices))
	for i, d := range devices {
		stateDevices[i] = state.DeviceInfo{
			DevicePath:  d.DevicePath,
			DeviceName:  d.DeviceName,
			Size:        d.Size,
			SizeHuman:   d.SizeHuman,
			IsUSB:       d.IsUSB,
			IsMounted:   d.IsMounted,
			MountPath:   d.MountPath,
			HasDCIM:     d.HasDCIM,
			VolumeLabel: d.VolumeLabel,
		}
	}
	if err := s.stateMgr.SetAvailableDevices(stateDevices); err != nil {
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

	log.Println("Daemon shutdown complete")
}
