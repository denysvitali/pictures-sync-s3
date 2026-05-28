package mock

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
)

// MockBackend provides realistic test data for UI development and testing
type MockBackend struct {
	currentState    *state.CurrentState
	syncHistory     []state.SyncRecord
	wifiNetworks    []SafeNetworkInfo
	availableWiFi   []wifimanager.ScanResult
	currentSync     *state.SyncRecord
	rcloneConfig    string
	isWSTokenValid  bool
	files           []MockFile
	devicesList     []state.DeviceInfo
	cancelChan      chan bool
}

// SafeNetworkInfo represents network information without credentials (copied from handlers)
type SafeNetworkInfo struct {
	SSID        string `json:"ssid"`
	HasPassword bool   `json:"has_password"`
}

// MockFile represents a file in the mock gallery
type MockFile struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	IsDir        bool      `json:"is_dir"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"`
}

// NewMockBackend creates a new mock backend with realistic test data
func NewMockBackend() *MockBackend {
	mock := &MockBackend{
		isWSTokenValid: true,
	}
	mock.initializeTestData()
	return mock
}

// initializeTestData creates comprehensive test data
func (m *MockBackend) initializeTestData() {
	// Initialize current state
	m.currentState = &state.CurrentState{
		Status:        state.StatusIdle,
		SDCardMounted: true,
		SDCardPath:    "/perm/pictures-sync/mounts/sdcard",
		AvailableDevices: []state.DeviceInfo{
			{
				DevicePath:  "/dev/mmcblk0p1",
				DeviceName:  "SanDisk Ultra 32GB",
				Size:        32000000000,
				SizeHuman:   "32.0 GB",
				IsUSB:       false,
				IsMounted:   true,
				MountPath:   "/perm/pictures-sync/mounts/sdcard",
				HasDCIM:     true,
				VolumeLabel: "CANON_DC",
			},
			{
				DevicePath:  "/dev/sda1",
				DeviceName:  "Kingston DataTraveler",
				Size:        16000000000,
				SizeHuman:   "16.0 GB",
				IsUSB:       true,
				IsMounted:   false,
				MountPath:   "",
				HasDCIM:     false,
				VolumeLabel: "KINGSTON",
			},
		},
		NeedsDeviceSelect: false,
	}

	// Initialize sync history
	m.syncHistory = []state.SyncRecord{
		{
			ID:              "sync-20241018-001",
			StartTime:       time.Now().Add(-2 * time.Hour),
			EndTime:         time.Now().Add(-2*time.Hour + 45*time.Minute),
			Status:          "success",
			FilesTotal:      247,
			FilesSynced:     247,
			BytesTotal:      1024 * 1024 * 1024 * 2, // 2GB
			BytesSynced:     1024 * 1024 * 1024 * 2,
			CardID:          "card-a1b2c3d4",
			TransferSpeed:   12.5 * 1024 * 1024, // 12.5 MB/s
		},
		{
			ID:              "sync-20241017-003",
			StartTime:       time.Now().Add(-25 * time.Hour),
			EndTime:         time.Now().Add(-25*time.Hour + 32*time.Minute),
			Status:          "success",
			FilesTotal:      189,
			FilesSynced:     189,
			BytesTotal:      1024 * 1024 * 1024 * 1, // 1.5GB
			BytesSynced:     1024 * 1024 * 1024 * 1,
			CardID:          "card-e5f6g7h8",
			TransferSpeed:   8.2 * 1024 * 1024, // 8.2 MB/s
		},
		{
			ID:              "sync-20241017-002",
			StartTime:       time.Now().Add(-26 * time.Hour),
			EndTime:         time.Now().Add(-26*time.Hour + 5*time.Minute),
			Status:          "error",
			FilesTotal:      156,
			FilesSynced:     23,
			BytesTotal:      1024 * 1024 * 700, // 700MB
			BytesSynced:     1024 * 1024 * 95,  // 95MB
			Error:           "Network connection lost during sync",
			CardID:          "card-e5f6g7h8",
			TransferSpeed:   2.1 * 1024 * 1024, // 2.1 MB/s
		},
		{
			ID:              "sync-20241016-001",
			StartTime:       time.Now().Add(-48 * time.Hour),
			EndTime:         time.Now().Add(-48*time.Hour + 67*time.Minute),
			Status:          "success",
			FilesTotal:      423,
			FilesSynced:     423,
			BytesTotal:      1024 * 1024 * 1024 * 3, // 3GB
			BytesSynced:     1024 * 1024 * 1024 * 3,
			CardID:          "card-i9j0k1l2",
			TransferSpeed:   15.8 * 1024 * 1024, // 15.8 MB/s
		},
	}

	// Set last sync
	if len(m.syncHistory) > 0 {
		m.currentState.LastSync = &m.syncHistory[0]
	}

	// Initialize WiFi networks (saved)
	m.wifiNetworks = []SafeNetworkInfo{
		{SSID: "Home_WiFi_5G", HasPassword: true},
		{SSID: "Office_Network", HasPassword: true},
		{SSID: "Guest_Network", HasPassword: false},
	}

	// Initialize available WiFi networks (scan results)
	m.availableWiFi = []wifimanager.ScanResult{
		{SSID: "Home_WiFi_5G", Signal: -35, Encrypted: true},
		{SSID: "Home_WiFi_2.4G", Signal: -42, Encrypted: true},
		{SSID: "Office_Network", Signal: -48, Encrypted: true},
		{SSID: "Neighbor_WiFi", Signal: -65, Encrypted: true},
		{SSID: "Guest_Network", Signal: -38, Encrypted: false},
		{SSID: "Coffee_Shop_Free", Signal: -72, Encrypted: false},
		{SSID: "Mobile_Hotspot", Signal: -58, Encrypted: true},
		{SSID: "NETGEAR_5G", Signal: -75, Encrypted: true},
	}

	// Initialize rclone config
	m.rcloneConfig = `[backblaze]
type = b2
account = 1234567890abcdef
key = K001234567890123456789012345678901234567890
endpoint = https://s3.us-west-002.backblazeb2.com

[google-photos]
type = googlephotos
client_id = 123456789-abcdefghijklmnop.apps.googleusercontent.com
client_secret = GOCSPX-1234567890123456789012345678901234
token = {"access_token":"ya29.a0A...","token_type":"Bearer","refresh_token":"1//0G...","expiry":"2024-10-19T10:30:00Z"}`

	// Initialize mock files for gallery
	m.files = []MockFile{
		{Name: "DCIM", IsDir: true, ModTime: time.Now().Add(-24 * time.Hour)},
		{Name: "card-a1b2c3d4", IsDir: true, ModTime: time.Now().Add(-2 * time.Hour)},
		{Name: "card-e5f6g7h8", IsDir: true, ModTime: time.Now().Add(-25 * time.Hour)},
		{Name: "card-i9j0k1l2", IsDir: true, ModTime: time.Now().Add(-48 * time.Hour)},
	}

	// Initialize devices list
	m.devicesList = m.currentState.AvailableDevices
}

// StartSyncSimulation simulates a sync in progress with realistic progress updates
func (m *MockBackend) StartSyncSimulation() {
	if m.currentSync != nil {
		return // Sync already running
	}

	// Create new sync record
	m.currentSync = &state.SyncRecord{
		ID:              fmt.Sprintf("sync-%s-mock", time.Now().Format("20060102-150405")),
		StartTime:       time.Now(),
		Status:          "syncing",
		FilesTotal:      312,
		FilesSynced:     0,
		BytesTotal:      1024 * 1024 * 1024 * 2, // 2GB
		BytesSynced:     0,
		CardID:          "card-mock-test",
		CurrentFile:     "",
		TransferSpeed:   0,
		ETA:             "",
	}

	m.currentState.Status = state.StatusSyncing
	m.currentState.CurrentSync = m.currentSync

	// Create cancel channel for this sync
	m.cancelChan = make(chan bool, 1)

	// Simulate progress updates
	go func() {
		defer func() {
			// Clean up cancel channel when goroutine exits
			if m.cancelChan != nil {
				close(m.cancelChan)
				m.cancelChan = nil
			}
		}()

		totalFiles := m.currentSync.FilesTotal
		totalBytes := m.currentSync.BytesTotal

		for i := int64(0); i <= totalFiles; i++ {
			// Check for cancellation
			select {
			case <-m.cancelChan:
				return // Sync was cancelled
			default:
			}

			if m.currentSync == nil {
				return // Sync was cancelled
			}

			// Update progress
			m.currentSync.FilesSynced = i
			m.currentSync.BytesSynced = (totalBytes * i) / totalFiles
			m.currentSync.CurrentFile = fmt.Sprintf("IMG_%04d.JPG", 1000+i)
			m.currentSync.CurrentFileSize = 4 * 1024 * 1024 // 4MB per file

			// Calculate transfer speed (simulate realistic variation)
			if i > 0 {
				elapsed := time.Since(m.currentSync.StartTime).Seconds()
				m.currentSync.TransferSpeed = float64(m.currentSync.BytesSynced) / elapsed

				// Calculate ETA
				if m.currentSync.TransferSpeed > 0 {
					remaining := float64(totalBytes - m.currentSync.BytesSynced)
					etaSeconds := remaining / m.currentSync.TransferSpeed
					m.currentSync.ETA = utils.FormatDurationTime(time.Duration(etaSeconds) * time.Second)
				}
			}

			// Random delay to simulate realistic file transfer speeds
			select {
			case <-m.cancelChan:
				return // Sync was cancelled
			// #nosec G404 -- weak random is acceptable for mock simulation timing
			case <-time.After(time.Duration(200+rand.Intn(300)) * time.Millisecond):
			}
		}

		// Complete sync
		if m.currentSync != nil {
			m.currentSync.EndTime = time.Now()
			m.currentSync.Status = "success"
			m.currentSync.FilesSynced = m.currentSync.FilesTotal
			m.currentSync.BytesSynced = m.currentSync.BytesTotal
			m.currentSync.ETA = ""

			// Add to history
			m.syncHistory = append([]state.SyncRecord{*m.currentSync}, m.syncHistory...)
			m.currentState.LastSync = m.currentSync

			// Clear current sync and set status
			m.currentSync = nil
			m.currentState.CurrentSync = nil
			m.currentState.Status = state.StatusSuccess

			// After 3 seconds, go back to idle
			select {
			case <-m.cancelChan:
				return
			case <-time.After(3 * time.Second):
				m.currentState.Status = state.StatusIdle
			}
		}
	}()
}

// CancelSync simulates cancelling a sync operation
func (m *MockBackend) CancelSync() {
	if m.currentSync != nil {
		// Signal cancellation to the goroutine
		if m.cancelChan != nil {
			select {
			case m.cancelChan <- true:
			default:
			}
		}

		m.currentSync.EndTime = time.Now()
		m.currentSync.Status = "cancelled"
		m.currentSync.Error = "Cancelled by user"
		m.currentSync.ETA = ""

		// Add to history
		m.syncHistory = append([]state.SyncRecord{*m.currentSync}, m.syncHistory...)

		// Set last sync and clear current sync
		m.currentState.LastSync = m.currentSync
		m.currentSync = nil
		m.currentState.CurrentSync = nil
		m.currentState.Status = state.StatusIdle
	}
}

// HTTP Handlers for mock endpoints

func (m *MockBackend) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	httputil.JSON(w, http.StatusOK,m.currentState)
}

func (m *MockBackend) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	httputil.JSON(w, http.StatusOK,m.syncHistory)
}

func (m *MockBackend) HandleWSToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := websocket.CreateWSToken()
	httputil.JSON(w, http.StatusOK,map[string]string{
		"ws_token": token,
	})
}

func (m *MockBackend) HandleWiFiScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simulate scan delay
	time.Sleep(500 * time.Millisecond)

	// Randomize signal strengths slightly
	networks := make([]wifimanager.ScanResult, len(m.availableWiFi))
	copy(networks, m.availableWiFi)

	for i := range networks {
		// Add ±5 dB variation to signal strength
		// #nosec G404 -- weak random is acceptable for mock signal variation
		variation := rand.Intn(10) - 5
		networks[i].Signal += variation
	}

	httputil.JSON(w, http.StatusOK,map[string]interface{}{
		"networks": networks,
	})
}

func (m *MockBackend) HandleWiFiNetworks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httputil.JSON(w, http.StatusOK,map[string]interface{}{
		"networks": m.wifiNetworks,
	})
}

func (m *MockBackend) HandleWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Simulate connection delay
	time.Sleep(2 * time.Second)

	// Check if network requires password
	var requiresPassword bool
	for _, network := range m.availableWiFi {
		if network.SSID == req.SSID {
			requiresPassword = network.Encrypted
			break
		}
	}

	// Simulate authentication failure
	if requiresPassword && req.Password == "" {
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"success": false,
			"error":   "Password required for encrypted network",
		})
		return
	}

	if requiresPassword && req.Password == "wrongpassword" {
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"success": false,
			"error":   "Authentication failed: incorrect password",
		})
		return
	}

	// Add network to saved networks
	found := false
	for _, network := range m.wifiNetworks {
		if network.SSID == req.SSID {
			found = true
			break
		}
	}
	if !found {
		m.wifiNetworks = append(m.wifiNetworks, SafeNetworkInfo{
			SSID:        req.SSID,
			HasPassword: requiresPassword,
		})
	}

	httputil.JSON(w, http.StatusOK,map[string]interface{}{
		"success": true,
	})
}

func (m *MockBackend) HandleWiFiDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SSID string `json:"ssid"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Remove network from saved networks
	for i, network := range m.wifiNetworks {
		if network.SSID == req.SSID {
			m.wifiNetworks = append(m.wifiNetworks[:i], m.wifiNetworks[i+1:]...)
			break
		}
	}

	httputil.JSON(w, http.StatusOK,map[string]interface{}{
		"success": true,
	})
}

func (m *MockBackend) HandleWiFiStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simulate being connected to the first saved network
	if len(m.wifiNetworks) > 0 {
		// Find signal strength for connected network
		signal := -45 // Default signal
		for _, available := range m.availableWiFi {
			if available.SSID == m.wifiNetworks[0].SSID {
				signal = available.Signal
				break
			}
		}

		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"connected": true,
			"ssid":      m.wifiNetworks[0].SSID,
			"signal":    signal,
		})
	} else {
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"connected": false,
			"error":     "Not connected to any network",
		})
	}
}

func (m *MockBackend) HandleWiFiReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SSIDs []string `json:"ssids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Reorder networks based on provided SSIDs
	reordered := make([]SafeNetworkInfo, 0, len(m.wifiNetworks))
	for _, ssid := range req.SSIDs {
		for _, network := range m.wifiNetworks {
			if network.SSID == ssid {
				reordered = append(reordered, network)
				break
			}
		}
	}
	m.wifiNetworks = reordered

	httputil.JSON(w, http.StatusOK,map[string]interface{}{
		"success": true,
	})
}

func (m *MockBackend) HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"config": m.rcloneConfig,
		})
	case http.MethodPost:
		var req struct {
			Config string `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Simulate validation delay
		time.Sleep(500 * time.Millisecond)

		// Simple validation - check for required sections
		if !strings.Contains(req.Config, "[") || !strings.Contains(req.Config, "]") {
			httputil.JSON(w, http.StatusOK,map[string]interface{}{
				"success": false,
				"error":   "Invalid rclone config format: no sections found",
			})
			return
		}

		m.rcloneConfig = req.Config
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"success": true,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *MockBackend) HandleConfigTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simulate test delay
	time.Sleep(2 * time.Second)

	// Simulate random test results
	// #nosec G404 -- weak random is acceptable for mock test simulation
	if rand.Float32() < 0.8 { // 80% success rate
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"success": true,
			"message": "Configuration test successful! All remotes are accessible.",
		})
	} else {
		httputil.JSON(w, http.StatusOK,map[string]interface{}{
			"success": false,
			"error":   "Test failed: Unable to connect to backblaze remote. Please check your credentials.",
		})
	}
}

func (m *MockBackend) HandleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	httputil.JSON(w, http.StatusOK,m.devicesList)
}

func (m *MockBackend) HandleSyncStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if m.currentSync != nil {
		http.Error(w, "Sync already in progress", http.StatusConflict)
		return
	}

	if !m.currentState.SDCardMounted {
		http.Error(w, "No SD card mounted", http.StatusBadRequest)
		return
	}

	m.StartSyncSimulation()

	httputil.JSON(w, http.StatusOK,map[string]string{
		"status":  "ok",
		"message": "Sync started",
	})
}

func (m *MockBackend) HandleSyncCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if m.currentSync == nil {
		http.Error(w, "No sync in progress", http.StatusBadRequest)
		return
	}

	m.CancelSync()

	httputil.JSON(w, http.StatusOK,map[string]string{
		"status":  "ok",
		"message": "Sync cancelled",
	})
}

func (m *MockBackend) HandleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path parameter from query
	path := r.URL.Query().Get("path")

	var files []MockFile
	if path == "" || path == "/" {
		// Root level - show card directories
		files = m.files
	} else {
		// Inside a card directory - show sample photos
		files = []MockFile{
			{Name: "IMG_0001.JPG", Size: 4 * 1024 * 1024, ModTime: time.Now().Add(-1 * time.Hour), IsDir: false},
			{Name: "IMG_0002.JPG", Size: 3984588, ModTime: time.Now().Add(-2 * time.Hour), IsDir: false},
			{Name: "IMG_0003.JPG", Size: 4404019, ModTime: time.Now().Add(-3 * time.Hour), IsDir: false},
			{Name: "IMG_0004.MP4", Size: 45 * 1024 * 1024, ModTime: time.Now().Add(-4 * time.Hour), IsDir: false},
			{Name: "IMG_0005.JPG", Size: 4089446, ModTime: time.Now().Add(-5 * time.Hour), IsDir: false},
		}
	}

	httputil.JSON(w, http.StatusOK,map[string]interface{}{
		"files": files,
		"path":  path,
	})
}

// RegisterHandlers registers all mock handlers with the provided mux
func (m *MockBackend) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", m.HandleStatus)
	mux.HandleFunc("/api/history", m.HandleHistory)
	mux.HandleFunc("/api/ws-token", m.HandleWSToken)
	mux.HandleFunc("/api/wifi/scan", m.HandleWiFiScan)
	mux.HandleFunc("/api/wifi/networks", m.HandleWiFiNetworks)
	mux.HandleFunc("/api/wifi/connect", m.HandleWiFiConnect)
	mux.HandleFunc("/api/wifi/disconnect", m.HandleWiFiDisconnect)
	mux.HandleFunc("/api/wifi/status", m.HandleWiFiStatus)
	mux.HandleFunc("/api/wifi/reorder", m.HandleWiFiReorder)
	mux.HandleFunc("/api/config", m.HandleConfig)
	mux.HandleFunc("/api/config/test", m.HandleConfigTest)
	mux.HandleFunc("/api/devices", m.HandleDevices)
	mux.HandleFunc("/api/sync/start", m.HandleSyncStart)
	mux.HandleFunc("/api/sync/cancel", m.HandleSyncCancel)
	mux.HandleFunc("/api/files", m.HandleFiles)
}