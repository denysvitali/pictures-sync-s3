package syncmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/operations"
)

const publicLinkExpiry = 15 * time.Minute
const defaultFileDownloadTimeout = 60 * time.Second

// FileInfo represents a file or directory on the remote
type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

// FileListResult represents paginated file listing result
type FileListResult struct {
	Files      []FileInfo `json:"files"`
	Path       string     `json:"path"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalPages int        `json:"total_pages"`
	HasMore    bool       `json:"has_more"`
}

// TestConnection tests the rclone configuration
func (m *Manager) TestConnection() error {
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	// Load rclone config from custom path
	if err := m.loadRcloneConfigLocked(); err != nil {
		return err
	}

	ctx := context.Background()

	m.mu.Lock()
	remoteName := m.remoteName
	remotePath := m.remotePath
	m.mu.Unlock()

	// Try to create the configured destination filesystem. Listing the remote
	// root can fail for bucket-scoped B2 application keys even when the backup
	// destination itself is accessible.
	destination := remoteName + ":" + remotePath
	fsys, err := fs.NewFs(ctx, destination)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	// Try to list the root directory (using a buffer to capture output)
	var buf bytes.Buffer
	if err := operations.List(ctx, fsys, &buf); err != nil {
		return fmt.Errorf("failed to list remote: %w", err)
	}

	log.Printf("Connection test successful")
	return nil
}

// ListRemotes lists configured remotes
func (m *Manager) ListRemotes() ([]string, error) {
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	// Load rclone config from custom path
	if err := m.loadRcloneConfigLocked(); err != nil {
		return nil, err
	}

	// Get the storage from the configuration (already under rcloneConfigMu)
	storage, err := m.getConfigStorageLocked()
	if err != nil {
		return nil, err
	}

	// Get all configured remotes from the config file
	sections := storage.GetSectionList()

	remotes := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			remotes = append(remotes, section)
		}
	}

	return remotes, nil
}

// ListCardIDs lists all card IDs (card-* directories) in the photos folder
func (m *Manager) ListCardIDs() ([]FileInfo, error) {
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	// Load rclone config
	if err := m.loadRcloneConfigLocked(); err != nil {
		return nil, err
	}

	// Create context with 25 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// List root photos directory
	fullPath := m.remoteName + ":" + m.remotePath
	fsys, err := fs.NewFs(ctx, fullPath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout accessing remote path (check network and remote configuration)")
		}
		return nil, fmt.Errorf("failed to access remote path: %w", err)
	}

	entries, err := fsys.List(ctx, "")
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout listing card directories (remote may be slow or unreachable)")
		}
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	var cardDirs []FileInfo
	for _, entry := range entries {
		// Check for timeout during iteration
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timeout while processing card directories")
		}

		if dir, ok := entry.(fs.Directory); ok {
			name := dir.Remote()
			// Only include card-* directories
			if strings.HasPrefix(name, "card-") {
				cardDirs = append(cardDirs, FileInfo{
					Name:    name,
					Path:    name,
					Size:    0,
					ModTime: dir.ModTime(ctx),
					IsDir:   true,
				})
			}
		}
	}

	// Sort by modification time (most recent first)
	sort.Slice(cardDirs, func(i, j int) bool {
		return cardDirs[i].ModTime.After(cardDirs[j].ModTime)
	})

	return cardDirs, nil
}

// ListFilesPaginated lists files with pagination support.
// It iterates rclone's directory listing exactly once, converting only the
// entries that fall within the requested page window to FileInfo values.
// Entries outside the window are counted but not materialised, so peak
// memory scales with pageSize rather than the total number of remote entries.
func (m *Manager) ListFilesPaginated(reqPath string, page, pageSize int) (*FileListResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 100 // Default page size
	}

	// Validate and clean path (mirror ListFiles).
	if strings.Contains(reqPath, "..") {
		return nil, fmt.Errorf("invalid path: contains directory traversal")
	}
	cleanedPath := strings.TrimPrefix(strings.TrimPrefix(filepath.Clean(reqPath), "/"), "\\")

	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	if err := m.loadRcloneConfigLocked(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	var fullPath string
	if cleanedPath == "" || cleanedPath == "/" {
		fullPath = m.remoteName + ":" + m.remotePath
	} else {
		fullPath = m.remoteName + ":" + filepath.Join(m.remotePath, cleanedPath)
	}

	fsys, err := fs.NewFs(ctx, fullPath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout accessing remote path (check network and remote configuration)")
		}
		return nil, fmt.Errorf("failed to access remote path: %w", err)
	}

	entries, err := fsys.List(ctx, "")
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout listing files (remote may be slow or unreachable)")
		}
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	// Single pass: count total and collect only the page window.
	start := (page - 1) * pageSize
	end := start + pageSize
	total := len(entries)

	pageFiles := make([]FileInfo, 0, min(pageSize, max(0, total-start)))
	for i, entry := range entries {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timeout while processing files")
		}
		if i < start || i >= end {
			// Outside the page window — count only, do not convert.
			continue
		}
		switch item := entry.(type) {
		case fs.Directory:
			name := item.Remote()
			itemPath := name
			if cleanedPath != "" && cleanedPath != "/" {
				itemPath = filepath.Join(cleanedPath, name)
			}
			pageFiles = append(pageFiles, FileInfo{
				Name:    name,
				Path:    itemPath,
				Size:    0,
				ModTime: item.ModTime(ctx),
				IsDir:   true,
			})
		case fs.Object:
			name := item.Remote()
			itemPath := name
			if cleanedPath != "" && cleanedPath != "/" {
				itemPath = filepath.Join(cleanedPath, name)
			}
			pageFiles = append(pageFiles, FileInfo{
				Name:    name,
				Path:    itemPath,
				Size:    item.Size(),
				ModTime: item.ModTime(ctx),
				IsDir:   false,
			})
		}
	}

	totalPages := (total + pageSize - 1) / pageSize
	return &FileListResult{
		Files:      pageFiles,
		Path:       reqPath,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasMore:    page < totalPages,
	}, nil
}

// ListFiles lists files and directories at the given path on the remote
func (m *Manager) ListFiles(path string) ([]FileInfo, error) {
	// Validate path to prevent directory traversal
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path: contains directory traversal")
	}

	// Clean the path to remove any potential traversal attempts
	path = filepath.Clean(path)

	// Ensure path doesn't start with / or \ (should be relative)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "\\")

	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	// Load rclone config from custom path
	if err := m.loadRcloneConfigLocked(); err != nil {
		return nil, err
	}

	// Create context with 25 second timeout (less than client's 30s timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Construct full remote path
	var fullPath string
	if path == "" || path == "/" {
		fullPath = m.remoteName + ":" + m.remotePath
	} else {
		fullPath = m.remoteName + ":" + filepath.Join(m.remotePath, path)
	}

	// Create remote filesystem
	fsys, err := fs.NewFs(ctx, fullPath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout accessing remote path (check network and remote configuration)")
		}
		return nil, fmt.Errorf("failed to access remote path: %w", err)
	}

	var files []FileInfo

	// Use List to get both files and directories at current level
	entries, err := fsys.List(ctx, "")
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout listing files (remote may be slow or unreachable)")
		}
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	for _, entry := range entries {
		// Check for timeout during iteration
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timeout while processing files")
		}

		switch item := entry.(type) {
		case fs.Directory:
			name := item.Remote()
			// Build full path relative to remote root
			itemPath := name
			if path != "" && path != "/" {
				itemPath = filepath.Join(path, name)
			}
			files = append(files, FileInfo{
				Name:    name,
				Path:    itemPath,
				Size:    0,
				ModTime: item.ModTime(ctx),
				IsDir:   true,
			})
		case fs.Object:
			name := item.Remote()
			// Build full path relative to remote root
			itemPath := name
			if path != "" && path != "/" {
				itemPath = filepath.Join(path, name)
			}
			files = append(files, FileInfo{
				Name:    name,
				Path:    itemPath,
				Size:    item.Size(),
				ModTime: item.ModTime(ctx),
				IsDir:   false,
			})
		}
	}

	return files, nil
}

// GetFile retrieves a file from the remote and writes it to the provided writer
func (m *Manager) GetFile(path string, w io.Writer) error {
	return m.getFile(path, w, defaultFileDownloadTimeout)
}

// GetFileWithTimeout retrieves a file from the remote using the provided
// timeout. It is intended for background transfers where large video files can
// legitimately take longer than the interactive file-view timeout.
func (m *Manager) GetFileWithTimeout(path string, w io.Writer, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultFileDownloadTimeout
	}
	return m.getFile(path, w, timeout)
}

// GetFileRange streams at most maxBytes of a remote file into w. It is used for
// cheap thumbnailing: fetching only the leading bytes of a JPEG is enough to
// read the embedded EXIF thumbnail without downloading the full-resolution
// image. When maxBytes <= 0 the whole file is streamed.
func (m *Manager) GetFileRange(path string, w io.Writer, maxBytes int64) error {
	if maxBytes <= 0 {
		return m.getFile(path, w, defaultFileDownloadTimeout)
	}
	err := m.getFile(path, &limitedWriter{w: w, remaining: maxBytes}, defaultFileDownloadTimeout)
	if errors.Is(err, errWriteLimitReached) {
		// We deliberately stopped after maxBytes — not a real failure.
		return nil
	}
	return err
}

// limitedWriter writes at most remaining bytes to the underlying writer and
// silently discards the rest, signalling the copy loop to stop by returning an
// error once the cap is hit so the upstream download can be torn down early.
type limitedWriter struct {
	w         io.Writer
	remaining int64
}

// errWriteLimitReached is returned by limitedWriter once its cap is exhausted so
// the io.Copy in getFile stops pulling bytes from the remote.
var errWriteLimitReached = errors.New("write limit reached")

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, errWriteLimitReached
	}
	if int64(len(p)) > l.remaining {
		n, err := l.w.Write(p[:l.remaining])
		l.remaining -= int64(n)
		if err != nil {
			return n, err
		}
		// Report the full length so io.Copy treats the chunk as consumed, then
		// stop on the next call. Returning a short count would make io.Copy emit
		// ErrShortWrite instead of our sentinel.
		return len(p), errWriteLimitReached
	}
	n, err := l.w.Write(p)
	l.remaining -= int64(n)
	return n, err
}

func (m *Manager) getFile(path string, w io.Writer, timeout time.Duration) error {
	// Validate path to prevent directory traversal
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path: contains directory traversal")
	}

	// Clean the path to remove any potential traversal attempts
	path = filepath.Clean(path)

	// Ensure path doesn't start with / or \ (should be relative)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "\\")

	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	// Load rclone config from custom path
	if err := m.loadRcloneConfigLocked(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Split path into directory and filename
	dir := filepath.Dir(path)
	filename := filepath.Base(path)

	// Construct the directory path on remote
	var remoteDirPath string
	if dir == "." || dir == "/" {
		// File is at root of remotePath
		remoteDirPath = m.remoteName + ":" + m.remotePath
	} else {
		// File is in a subdirectory
		remoteDirPath = m.remoteName + ":" + filepath.Join(m.remotePath, dir)
	}

	// Create filesystem for the directory containing the file
	fsys, err := fs.NewFs(ctx, remoteDirPath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout accessing remote directory (check network and remote configuration)")
		}
		return fmt.Errorf("failed to access remote directory %s: %w", remoteDirPath, err)
	}

	// Get the file object
	obj, err := fsys.NewObject(ctx, filename)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout getting file object (remote may be slow or unreachable)")
		}
		return fmt.Errorf("failed to get file object %s in %s: %w", filename, remoteDirPath, err)
	}

	// Open the file for reading
	rc, err := obj.Open(ctx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timeout opening file (remote may be slow or unreachable)")
		}
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer rc.Close()

	// Copy the file content to the writer with timeout awareness
	type copyResult struct {
		n   int64
		err error
	}
	resultChan := make(chan copyResult, 1)

	go func() {
		n, err := io.Copy(w, rc)
		resultChan <- copyResult{n, err}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout downloading file after %s", timeout)
	case result := <-resultChan:
		if result.err != nil {
			return fmt.Errorf("failed to copy file content: %w", result.err)
		}
		return nil
	}
}

// GetPublicLink returns a temporary cloud-provider URL for a file on the remote.
func (m *Manager) GetPublicLink(path string) (string, error) {
	// Validate path to prevent directory traversal
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("invalid path: contains directory traversal")
	}

	// Clean the path to remove any potential traversal attempts
	path = filepath.Clean(path)

	// Ensure path doesn't start with / or \ (should be relative)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "\\")
	if path == "" || path == "." {
		return "", fmt.Errorf("path must reference a file")
	}

	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	// Load rclone config from custom path
	if err := m.loadRcloneConfigLocked(); err != nil {
		return "", err
	}

	// Create context with 25 second timeout (less than client's 30s timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	m.mu.Lock()
	remoteName := m.remoteName
	remotePath := m.remotePath
	m.mu.Unlock()

	fsys, err := fs.NewFs(ctx, remoteName+":"+remotePath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout accessing remote path (check network and remote configuration)")
		}
		return "", fmt.Errorf("failed to access remote path: %w", err)
	}

	link, err := operations.PublicLink(ctx, fsys, path, fs.Duration(publicLinkExpiry), false)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout creating public link (remote may be slow or unreachable)")
		}
		return "", fmt.Errorf("failed to create public link: %w", err)
	}

	return link, nil
}

// getConfigStorage loads and returns the configuration storage. It acquires
// rcloneConfigMu for its lifetime; the returned storage is only valid while
// the caller holds the mutex (which this helper does NOT keep held).
//
// Prefer getConfigStorageLocked when the caller already owns rcloneConfigMu.
func (m *Manager) getConfigStorage() (*configfile.Storage, error) {
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	return m.getConfigStorageLocked()
}

// getConfigStorageLocked loads and returns the configuration storage.
// Caller MUST hold rcloneConfigMu.
func (m *Manager) getConfigStorageLocked() (*configfile.Storage, error) {
	if err := m.loadRcloneConfigLocked(); err != nil {
		return nil, err
	}

	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	return storage, nil
}
