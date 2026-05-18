package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/version"
)

const goZeroPseudoVersion = "v0.0.0-00010101000000-000000000000"

type otaReleaseCandidate struct {
	TagName         string    `json:"tag_name"`
	Name            string    `json:"name"`
	PublishedAt     time.Time `json:"published_at"`
	TargetCommitish string    `json:"target_commitish,omitempty"`
	AssetName       string    `json:"asset_name"`
	AssetSize       int64     `json:"asset_size"`
	AssetURL        string    `json:"asset_url"`
	HTMLURL         string    `json:"html_url"`
	Installed       bool      `json:"installed"`
}

type otaInstallHistoryItem struct {
	Release    string `json:"release"`
	Asset      string `json:"asset"`
	AssetURL   string `json:"asset_url"`
	State      string `json:"state"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type otaABPartitionMeta struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SizeHuman string `json:"size_human"`
}

type otaABPartitionStatus struct {
	Active       string             `json:"active"`
	Inactive     string             `json:"inactive"`
	UpdateSlot   string             `json:"update_slot"`
	Source       string             `json:"source"`
	ActiveInfo   otaABPartitionMeta `json:"active_info"`
	InactiveInfo otaABPartitionMeta `json:"inactive_info"`
	UpdateInfo   otaABPartitionMeta `json:"update_info"`
}

type otaStatusResponse struct {
	ota.Status
	CurrentVersion    string                  `json:"current_version"`
	CurrentCommit     string                  `json:"current_commit"`
	CurrentBuildDate  string                  `json:"current_build_date"`
	CurrentGoVersion  string                  `json:"current_go_version"`
	CurrentModule     string                  `json:"current_module"`
	CurrentDirty      bool                    `json:"current_dirty"`
	ABPartitions      otaABPartitionStatus    `json:"ab_partitions"`
	LatestVersion     string                  `json:"latest_version"`
	InstalledVersions []string                `json:"installed_versions"`
	Releases          []otaReleaseCandidate   `json:"releases"`
	InstallHistory    []otaInstallHistoryItem `json:"install_history"`
	UpdateAvailable   bool                    `json:"update_available"`
}

type otaInstallRequest struct {
	ReleaseTag string `json:"release_tag"`
}

func (ctx *Context) HandleOTAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.MethodNotAllowed(w)
		return
	}
	if ctx.OTAMgr == nil {
		httputil.ServiceUnavailable(w, "OTA manager not initialized")
		return
	}

	current := version.Get()
	response := otaStatusResponse{
		Status:           ctx.OTAMgr.Status(),
		CurrentVersion:   current.Version,
		CurrentCommit:    current.Commit,
		CurrentBuildDate: current.BuildDate,
		CurrentGoVersion: current.GoVersion,
		CurrentModule:    current.Module,
		CurrentDirty:     current.Dirty,
		ABPartitions:     getABPartitions(),
	}
	for _, versionName := range collectKnownVersions(ctx.OTAMgr, current.Version) {
		if versionName != "" {
			response.InstalledVersions = append(response.InstalledVersions, versionName)
		}
	}
	response.InstallHistory = collectInstallHistory(ctx.OTAMgr)

	releases, err := ctx.OTAMgr.AvailableReleases(r.Context())
	if err == nil {
		response.Releases = make([]otaReleaseCandidate, 0, len(releases))
		installedAt := parseBuildDate(current.BuildDate)

		for _, candidate := range releases {
			asset := candidate.Assets[0]
			option := otaReleaseCandidate{
				TagName:         candidate.TagName,
				Name:            candidate.Name,
				PublishedAt:     candidate.PublishedAt,
				TargetCommitish: candidate.TargetCommitish,
				AssetName:       asset.Name,
				AssetSize:       asset.Size,
				AssetURL:        asset.BrowserDownloadURL,
				HTMLURL:         candidate.HTMLURL,
			}
			releaseCommit := candidate.TargetCommitish
			if !response.UpdateAvailable && shouldResolveReleaseTagCommit(
				current.Version,
				current.Commit,
				installedAt,
				option.TagName,
				releaseCommit,
				option.PublishedAt,
			) {
				if tagCommit, err := ctx.OTAMgr.ReleaseTagCommit(r.Context(), option.TagName); err == nil {
					releaseCommit = tagCommit
				}
			}
			if isReleaseInstalled(current.Version, current.Commit, option.TagName, releaseCommit) {
				option.Installed = true
			}
			if response.LatestVersion == "" {
				response.LatestVersion = option.TagName
			}
			if response.UpdateAvailable == false {
				response.UpdateAvailable = isReleaseUpdateAvailable(
					current.Version,
					current.Commit,
					installedAt,
					option.TagName,
					releaseCommit,
					option.PublishedAt,
				)
			}

			response.Releases = append(response.Releases, option)
		}
	}

	httputil.JSON(w, http.StatusOK, response)
}

func (ctx *Context) HandleOTAInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.MethodNotAllowed(w)
		return
	}
	if ctx.OTAMgr == nil {
		httputil.ServiceUnavailable(w, "OTA manager not initialized")
		return
	}
	var req otaInstallRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httputil.BadRequest(w, "Invalid OTA install request")
			return
		}
	}

	status, err := ctx.OTAMgr.StartWithRelease(r.Context(), req.ReleaseTag)
	if err != nil {
		httputil.Error(w, http.StatusConflict, err.Error())
		return
	}
	httputil.JSON(w, http.StatusAccepted, status)
}

func parseBuildDate(buildDate string) time.Time {
	if buildDate == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, buildDate)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func isReleaseUpdateAvailable(currentVersion, currentCommit string, installedAt time.Time, releaseTag, releaseCommit string, publishedAt time.Time) bool {
	if isReleaseInstalled(currentVersion, currentCommit, releaseTag, releaseCommit) {
		return false
	}
	if installedAt.IsZero() {
		return strings.TrimSpace(releaseTag) != ""
	}
	return publishedAt.After(installedAt)
}

func sameReleaseVersion(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func isReleaseInstalled(currentVersion, currentCommit, releaseTag, releaseCommit string) bool {
	if sameReleaseVersion(releaseTag, currentVersion) {
		return true
	}
	return sameCommit(currentBuildCommit(currentVersion, currentCommit), releaseCommit)
}

func shouldResolveReleaseTagCommit(currentVersion, currentCommit string, installedAt time.Time, releaseTag, releaseCommit string, publishedAt time.Time) bool {
	if isReleaseInstalled(currentVersion, currentCommit, releaseTag, releaseCommit) {
		return false
	}
	if currentBuildCommit(currentVersion, currentCommit) == "" {
		return false
	}
	if normalizeCommitish(releaseCommit) != "" {
		return false
	}
	if installedAt.IsZero() {
		return strings.TrimSpace(releaseTag) != ""
	}
	return publishedAt.After(installedAt)
}

func currentBuildCommit(currentVersion, currentCommit string) string {
	if commit := commitFromVersion(currentVersion); commit != "" {
		return commit
	}
	return normalizeCommitish(currentCommit)
}

func commitFromVersion(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "master-") {
		return normalizeCommitish(strings.TrimPrefix(value, "master-"))
	}
	return normalizeCommitish(value)
}

func sameCommit(a, b string) bool {
	a = normalizeCommitish(a)
	b = normalizeCommitish(b)
	if a == "" || b == "" {
		return false
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(a, b)
}

func normalizeCommitish(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 7 || len(value) > 40 {
		return ""
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return value
}

func collectKnownVersions(otaManager *ota.Manager, currentVersion string) []string {
	history := otaManager.InstallationHistory()
	knownVersions := make([]string, 0, len(history)+1)
	knownSet := map[string]struct{}{}

	for i := len(history) - 1; i >= 0; i-- {
		if history[i].State != "installed" {
			continue
		}

		release := strings.TrimSpace(history[i].Release)
		if !isKnownInstalledVersion(release) {
			continue
		}
		if _, exists := knownSet[release]; exists {
			continue
		}

		knownVersions = append(knownVersions, release)
		knownSet[release] = struct{}{}
	}

	if isKnownInstalledVersion(currentVersion) {
		if _, exists := knownSet[currentVersion]; !exists {
			knownVersions = append(knownVersions, currentVersion)
		}
	}

	return knownVersions
}

func isKnownInstalledVersion(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "dev" && value != goZeroPseudoVersion
}

func collectInstallHistory(otaManager *ota.Manager) []otaInstallHistoryItem {
	history := otaManager.InstallationHistory()
	items := make([]otaInstallHistoryItem, 0, len(history))

	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Release == "" && entry.Message == "" {
			continue
		}

		items = append(items, otaInstallHistoryItem{
			Release:    entry.Release,
			Asset:      entry.Asset,
			AssetURL:   entry.AssetURL,
			State:      entry.State,
			Message:    entry.Message,
			Error:      entry.Error,
			StartedAt:  formatTimeForJSON(entry.StartedAt),
			FinishedAt: formatTimeForJSON(entry.FinishedAt),
		})
	}

	return items
}

func formatTimeForJSON(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func getABPartitions() otaABPartitionStatus {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return otaABPartitionStatus{Source: "proc_cmdline_unavailable"}
	}

	parts := strings.Fields(string(data))
	for _, part := range parts {
		if !strings.HasPrefix(part, "root=") {
			continue
		}

		rootValue := strings.TrimSpace(strings.TrimPrefix(part, "root="))
		activePartition := parseRootPartition(rootValue)
		activePath := resolveRootPartitionPath(rootValue)
		activeMeta := partitionMetadataFor(activePath, activePartition)
		if activePartition == 0 {
			return otaABPartitionStatus{
				Source: "proc_cmdline_unrecognized_root",
			}
		}
		if activePartition != 2 && activePartition != 3 {
			return otaABPartitionStatus{
				Source: "proc_cmdline_no_ab_partition",
			}
		}

		active := strconv.Itoa(activePartition)
		inactive := active
		if activePartition == 2 {
			inactive = "3"
		} else if activePartition == 3 {
			inactive = "2"
		}
		inactivePartition, _ := strconv.Atoi(inactive)

		inactiveMeta := partitionMetadataFor(inferPartitionPath(activePath, inactive), inactivePartition)

		return otaABPartitionStatus{
			Active:       active,
			Inactive:     inactive,
			UpdateSlot:   inactive,
			Source:       "proc_cmdline",
			ActiveInfo:   activeMeta,
			InactiveInfo: inactiveMeta,
			UpdateInfo:   inactiveMeta,
		}
	}

	return otaABPartitionStatus{Source: "proc_cmdline_root_missing"}
}

func parseRootPartition(rootValue string) int {
	rootValue = strings.TrimSpace(rootValue)
	if rootValue == "" {
		return 0
	}

	if strings.HasPrefix(rootValue, "PARTUUID=") {
		partUUID := strings.TrimPrefix(rootValue, "PARTUUID=")
		if part, ok := parsePARTNROFFPartition(partUUID); ok {
			return part
		}
		lastDash := strings.LastIndex(partUUID, "-")
		if lastDash >= 0 {
			if part, err := strconv.Atoi(partUUID[lastDash+1:]); err == nil && part > 0 {
				return part
			}
		}
	}

	lastP := strings.LastIndex(rootValue, "p")
	if lastP >= 0 && lastP+1 < len(rootValue) {
		if part, err := strconv.Atoi(rootValue[lastP+1:]); err == nil {
			return part
		}
	}

	lastDigitStart := len(rootValue)
	for lastDigitStart > 0 && rootValue[lastDigitStart-1] >= '0' && rootValue[lastDigitStart-1] <= '9' {
		lastDigitStart--
	}
	if lastDigitStart < len(rootValue) {
		if part, err := strconv.Atoi(rootValue[lastDigitStart:]); err == nil {
			return part
		}
	}

	return 0
}

func parsePARTNROFFPartition(partUUID string) (int, bool) {
	const marker = "/PARTNROFF="

	partUUID = strings.TrimSpace(partUUID)
	idx := strings.LastIndex(partUUID, marker)
	if idx < 0 {
		return 0, false
	}

	offsetValue := strings.TrimSpace(partUUID[idx+len(marker):])
	if offsetValue == "" {
		return 0, false
	}
	offset, err := strconv.Atoi(offsetValue)
	if err != nil || offset < 0 {
		return 0, false
	}

	return 1 + offset, true
}

func partitionMetadataFor(rootValue string, partition int) otaABPartitionMeta {
	if partition <= 0 {
		return otaABPartitionMeta{}
	}

	if rootValue == "" {
		return otaABPartitionMeta{
			SizeBytes: 0,
			SizeHuman: "unknown",
		}
	}

	size := partitionSizeBytes(rootValue)
	return otaABPartitionMeta{
		Path:      rootValue,
		SizeBytes: size,
		SizeHuman: formatBytes(size),
	}
}

func inferPartitionPath(rootValue, partition string) string {
	if partition == "" || rootValue == "" {
		return ""
	}

	n := len(rootValue)
	for n > 0 {
		if rootValue[n-1] >= '0' && rootValue[n-1] <= '9' {
			n--
			continue
		}
		break
	}

	if n >= len(rootValue) {
		return rootValue
	}

	return rootValue[:n] + partition
}

func resolveRootPartitionPath(rootValue string) string {
	rootValue = strings.TrimSpace(strings.Trim(rootValue, "\""))
	if rootValue == "" {
		return ""
	}

	if strings.HasPrefix(rootValue, "PARTUUID=") {
		partUUID := strings.TrimPrefix(rootValue, "PARTUUID=")
		partnoffPartition, hasPartnoff := parsePARTNROFFPartition(partUUID)
		if idx := strings.LastIndex(partUUID, "/PARTNROFF="); idx >= 0 {
			partUUID = partUUID[:idx]
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-partuuid", partUUID))
		if err == nil {
			if hasPartnoff {
				return inferPartitionPath(resolved, strconv.Itoa(partnoffPartition))
			}
			return resolved
		}
	}

	if strings.HasPrefix(rootValue, "/dev/") {
		return rootValue
	}

	return ""
}

func partitionSizeBytes(path string) int64 {
	name := filepath.Base(path)
	if name == "." || name == "/" {
		return 0
	}

	candidates := []string{
		filepath.Join("/sys/class/block", name, "size"),
		filepath.Join("/sys/block", name, "size"),
	}

	for _, candidate := range candidates {
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		parsed, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || parsed <= 0 {
			continue
		}

		return parsed * 512
	}

	parent := filepath.Dir(path)
	if parent != "/" && parent != "." && parent != "" {
		alt := filepath.Base(parent)
		candidate := filepath.Join("/sys/block", alt, filepath.Base(path), "size")
		raw, err := os.ReadFile(candidate)
		if err != nil {
			return 0
		}

		parsed, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || parsed <= 0 {
			return 0
		}

		return parsed * 512
	}

	return 0
}

func formatBytes(size int64) string {
	if size <= 0 {
		return "unknown"
	}

	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}

	return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
}
