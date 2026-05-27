package state

import (
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/systeminfo"
)

const CurrentStateSchemaVersion = 1

// SyncStatus represents the current sync operation status
type SyncStatus string

const (
	StatusIdle       SyncStatus = "idle"
	StatusDetected   SyncStatus = "detected"
	StatusSyncing    SyncStatus = "syncing"
	StatusCancelling SyncStatus = "cancelling"
	StatusSuccess    SyncStatus = "success"
	StatusError      SyncStatus = "error"
)

// SyncRecord represents a completed sync operation
type SyncRecord struct {
	ID              string    `json:"id"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	Status          string    `json:"status"`
	ProgressPhase   string    `json:"progress_phase,omitempty"`
	FilesTotal      int64     `json:"files_total"`
	FilesSynced     int64     `json:"files_synced"`
	BytesTotal      int64     `json:"bytes_total"`
	BytesSynced     int64     `json:"bytes_synced"`
	Error           string    `json:"error,omitempty"`
	CardID          string    `json:"card_id"` // Unique ID from .pictures-sync-id file
	CurrentFile     string    `json:"current_file,omitempty"`
	CurrentFileSize int64     `json:"current_file_size,omitempty"`
	TransferSpeed   float64   `json:"transfer_speed,omitempty"` // Bytes per second
	ETA             string    `json:"eta,omitempty"`            // Estimated time remaining (formatted)
}

// DeviceInfo represents a detected storage device
type DeviceInfo struct {
	DevicePath  string          `json:"device_path"`
	DeviceName  string          `json:"device_name"`
	Size        int64           `json:"size"`
	SizeHuman   string          `json:"size_human"`
	IsUSB       bool            `json:"is_usb"`
	IsMounted   bool            `json:"is_mounted"`
	MountPath   string          `json:"mount_path,omitempty"`
	HasDCIM     bool            `json:"has_dcim"`
	VolumeLabel string          `json:"volume_label,omitempty"`
	Partitions  []PartitionInfo `json:"partitions,omitempty"`
}

// PartitionInfo represents a partition on a detected storage device.
type PartitionInfo struct {
	DevicePath  string `json:"device_path"`
	DeviceName  string `json:"device_name"`
	Size        int64  `json:"size"`
	SizeHuman   string `json:"size_human"`
	FileSystem  string `json:"file_system,omitempty"`
	UUID        string `json:"uuid,omitempty"`
	VolumeLabel string `json:"volume_label,omitempty"`
	IsMounted   bool   `json:"is_mounted"`
	MountPath   string `json:"mount_path,omitempty"`
	HasDCIM     bool   `json:"has_dcim"`
}

// CurrentState represents the current system state
type CurrentState struct {
	SchemaVersion     int                     `json:"schema_version"`
	Status            SyncStatus              `json:"status"`
	Error             string                  `json:"error,omitempty"`
	CurrentSync       *SyncRecord             `json:"current_sync,omitempty"`
	LastSync          *SyncRecord             `json:"last_sync,omitempty"`
	SDCardMounted     bool                    `json:"sdcard_mounted"`
	SDCardPath        string                  `json:"sdcard_path,omitempty"`
	SDCardDevicePath  string                  `json:"sdcard_device_path,omitempty"`
	SDCardPhotoCount  int64                   `json:"sdcard_photo_count"`
	SDCardPhotoBytes  int64                   `json:"sdcard_photo_bytes"`
	AvailableDevices  []DeviceInfo            `json:"available_devices,omitempty"`
	NeedsDeviceSelect bool                    `json:"needs_device_select"`
	Runtime           *systeminfo.RuntimeInfo `json:"runtime,omitempty"`
}
