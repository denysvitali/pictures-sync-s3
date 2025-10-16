package daemon

import (
	"log"
	"net"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemon/cardhandler"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/ledcontroller"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/signals"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// Service represents the main photo backup daemon
type Service struct {
	eventMgr    *events.Manager
	stateMgr    *state.Manager
	settings    *settings.Settings
	syncMgr     *syncmanager.Manager
	ledCtrl     *ledcontroller.Controller
	monitor     *sdmonitor.Monitor
	cardHandler *cardhandler.Handler
	sigHandler  *signals.Handler
	timeSynced  bool // Track if time is properly synced
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
		time.Sleep(2 * time.Second)
	}

	// Wait for DNS to be available (check if we can resolve a common domain)
	log.Println("Checking DNS availability...")
	dnsReady := false
	for i := 0; i < 60; i++ { // Try for 2 minutes (60 * 2s)
		_, err := net.LookupHost("google.com")
		if err == nil {
			log.Println("DNS is ready")
			dnsReady = true
			break
		}
		// Only log every 5 attempts to reduce log spam
		if i%5 == 0 {
			log.Printf("DNS not ready yet (attempt %d/60): %v", i+1, err)
		}
		time.Sleep(2 * time.Second)
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

	return &Service{
		eventMgr:    eventMgr,
		stateMgr:    stateMgr,
		settings:    appSettings,
		syncMgr:     syncMgr,
		ledCtrl:     ledCtrl,
		monitor:     monitor,
		cardHandler: cardHandler,
		sigHandler:  sigHandler,
		timeSynced:  timeSynced,
	}, nil
}

// Run starts the daemon and blocks until shutdown signal is received
func (s *Service) Run() error {
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

// Shutdown gracefully shuts down the daemon
func (s *Service) Shutdown() {
	log.Println("Shutting down daemon...")

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
