package daemon

import (
	"log"
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
	time.Sleep(cfg.NTPSyncDelay)
	log.Println("Time sync wait complete. Current time:", time.Now())

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
