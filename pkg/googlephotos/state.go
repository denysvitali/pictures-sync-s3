package googlephotos

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const (
	googlePhotosStateFileName = "google-photos-sync-state.json"
	maxGooglePhotosHistory    = 100
	maxAlbumCacheEntries      = 64
	maxFileCacheEntries       = 10000
	// stateFlushInterval coalesces per-mutation disk writes. Saves still happen
	// promptly on lifecycle events (sync end, album creation, history append)
	// via flush(), but per-file mutations are batched to avoid SD-card wear and
	// to release the lock that 8 upload workers contend on.
	stateFlushInterval = 3 * time.Second
)

type persistedSyncState struct {
	FileCache    map[string]cachedFileMetadata `json:"file_cache,omitempty"`
	UploadTokens map[string]cachedUploadToken  `json:"upload_tokens,omitempty"`
	AlbumCache   []albumCacheEntry             `json:"album_cache,omitempty"`
	History      []SyncRunSummary              `json:"history,omitempty"`
	LastSuccess  *SyncRunSummary               `json:"last_success,omitempty"`
}

type cachedFileMetadata struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	Checksum   string    `json:"checksum,omitempty"`
	Mime       string    `json:"mime,omitempty"`
	Uploaded   bool      `json:"uploaded,omitempty"`
	UploadedAt time.Time `json:"uploaded_at,omitempty"`
}

type cachedUploadToken struct {
	Token     string    `json:"token"`
	FileName  string    `json:"file_name"`
	Checksum  string    `json:"checksum,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	BatchDone bool      `json:"batch_done,omitempty"`
}

type albumCacheEntry struct {
	CardID   string    `json:"card_id"`
	AlbumID  string    `json:"album_id"`
	Title    string    `json:"title"`
	LastUsed time.Time `json:"last_used"`
}

type stateStore struct {
	mu            sync.RWMutex
	path          string
	data          persistedSyncState
	checksumIndex map[string]string // sha256 → path, populated only for Uploaded entries
	dirty         bool
	lastFlush     time.Time
}

func newStateStore() *stateStore {
	return newStateStoreAt(filepath.Join(state.PermDir, googlePhotosStateFileName))
}

func newStateStoreAt(path string) *stateStore {
	store := &stateStore{
		path: path,
		data: persistedSyncState{
			FileCache:    make(map[string]cachedFileMetadata),
			UploadTokens: make(map[string]cachedUploadToken),
		},
		checksumIndex: make(map[string]string),
	}
	_ = utils.LoadJSON(store.path, &store.data, store.data)
	if store.data.FileCache == nil {
		store.data.FileCache = make(map[string]cachedFileMetadata)
	}
	if store.data.UploadTokens == nil {
		store.data.UploadTokens = make(map[string]cachedUploadToken)
	}
	for path, meta := range store.data.FileCache {
		if meta.Uploaded && meta.Checksum != "" {
			store.checksumIndex[meta.Checksum] = path
		}
	}
	return store
}

func (s *stateStore) fileMeta(path string) (cachedFileMetadata, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.data.FileCache[path]
	return meta, ok
}

func (s *stateStore) putFileMeta(meta cachedFileMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.FileCache[meta.Path] = meta
	if meta.Uploaded && meta.Checksum != "" {
		s.checksumIndex[meta.Checksum] = meta.Path
	}
	s.trimFileCacheLocked()
	s.maybeFlushLocked()
}

func (s *stateStore) rememberFileMeta(meta cachedFileMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.FileCache[meta.Path] = meta
	if meta.Uploaded && meta.Checksum != "" {
		s.checksumIndex[meta.Checksum] = meta.Path
	}
	s.trimFileCacheLocked()
	s.markDirtyLocked()
}

// flush forces a disk write. Used at sync lifecycle boundaries where durability
// matters (sync end, album creation, history append).
func (s *stateStore) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
}

// save is kept for backwards compatibility; routes through the debounced path.
func (s *stateStore) save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeFlushLocked()
}

func (s *stateStore) uploadToken(path string) (cachedUploadToken, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.data.UploadTokens[path]
	return token, ok
}

func (s *stateStore) uploadedChecksum(checksum string) bool {
	if checksum == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.checksumIndex[checksum]
	return ok
}

func (s *stateStore) putUploadToken(path string, token cachedUploadToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.UploadTokens[path] = token
	s.maybeFlushLocked()
}

func (s *stateStore) markBatchDone(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.data.UploadTokens[path]
	if ok {
		token.BatchDone = true
		s.data.UploadTokens[path] = token
	}
	meta, ok := s.data.FileCache[path]
	if ok {
		meta.Uploaded = true
		meta.UploadedAt = time.Now()
		s.data.FileCache[path] = meta
		if meta.Checksum != "" {
			s.checksumIndex[meta.Checksum] = path
		}
	}
	s.maybeFlushLocked()
}

func (s *stateStore) albumID(cardID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.AlbumCache {
		if s.data.AlbumCache[i].CardID == cardID {
			s.data.AlbumCache[i].LastUsed = time.Now()
			s.markDirtyLocked()
			return s.data.AlbumCache[i].AlbumID, true
		}
	}
	return "", false
}

func (s *stateStore) putAlbum(cardID, title, albumID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range s.data.AlbumCache {
		if s.data.AlbumCache[i].CardID == cardID {
			s.data.AlbumCache[i].AlbumID = albumID
			s.data.AlbumCache[i].Title = title
			s.data.AlbumCache[i].LastUsed = now
			s.flushLocked()
			return
		}
	}
	s.data.AlbumCache = append(s.data.AlbumCache, albumCacheEntry{CardID: cardID, AlbumID: albumID, Title: title, LastUsed: now})
	if len(s.data.AlbumCache) > maxAlbumCacheEntries {
		s.data.AlbumCache = s.data.AlbumCache[len(s.data.AlbumCache)-maxAlbumCacheEntries:]
	}
	s.flushLocked()
}

func (s *stateStore) addSummary(summary SyncRunSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.History = append([]SyncRunSummary{summary}, s.data.History...)
	if len(s.data.History) > maxGooglePhotosHistory {
		s.data.History = s.data.History[:maxGooglePhotosHistory]
	}
	if summary.Status == "completed" {
		copied := summary
		s.data.LastSuccess = &copied
	}
	s.flushLocked()
}

func (s *stateStore) summaries() ([]SyncRunSummary, *SyncRunSummary) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := make([]SyncRunSummary, len(s.data.History))
	copy(history, s.data.History)
	var last *SyncRunSummary
	if s.data.LastSuccess != nil {
		copied := *s.data.LastSuccess
		last = &copied
	}
	return history, last
}

func (s *stateStore) trimFileCacheLocked() {
	if len(s.data.FileCache) <= maxFileCacheEntries {
		return
	}
	removed := 0
	for path, meta := range s.data.FileCache {
		delete(s.data.FileCache, path)
		if meta.Checksum != "" {
			if owner, ok := s.checksumIndex[meta.Checksum]; ok && owner == path {
				delete(s.checksumIndex, meta.Checksum)
			}
		}
		removed++
		if len(s.data.FileCache) <= maxFileCacheEntries || removed > 1000 {
			return
		}
	}
}

func (s *stateStore) markDirtyLocked() {
	s.dirty = true
}

// maybeFlushLocked writes to disk only if the debounce interval has elapsed;
// otherwise it just marks state dirty for a later flush. Called with s.mu held.
func (s *stateStore) maybeFlushLocked() {
	s.dirty = true
	if time.Since(s.lastFlush) < stateFlushInterval {
		return
	}
	s.flushLocked()
}

func (s *stateStore) flushLocked() {
	if !s.dirty && !s.lastFlush.IsZero() {
		return
	}
	s.lastFlush = time.Now()
	s.dirty = false
	_ = utils.SaveJSON(s.path, &s.data, 0600)
}
