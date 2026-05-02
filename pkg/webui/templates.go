package webui

import (
	"embed"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

//go:embed dist
var assetsFS embed.FS

func HandleSPA(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		handleAssetOrIndex(w, r)
		return
	}

	content, err := assetsFS.ReadFile("dist/index.html")
	if err != nil {
		http.Error(w, "SPA index not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

func HandleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/legacy") {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/static/") {
		http.NotFound(w, r)
		return
	}

	handleAsset(w, r)
}

func handleAssetOrIndex(w http.ResponseWriter, r *http.Request) {
	if handleAsset(w, r) {
		return
	}

	content, err := assetsFS.ReadFile("dist/index.html")
	if err != nil {
		http.Error(w, "SPA index not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

func handleAsset(w http.ResponseWriter, r *http.Request) bool {
	rel := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if strings.HasPrefix(rel, "../") || rel == ".." || rel == "" {
		http.NotFound(w, r)
		return true
	}

	content, err := assetsFS.ReadFile("dist/" + filepath.ToSlash(rel))
	if err != nil {
		return false
	}

	if mimeType := mime.TypeByExtension(path.Ext(r.URL.Path)); mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.Copy(w, strings.NewReader(string(content)))
	return true
}

func HandleIndex(w http.ResponseWriter, r *http.Request)      { HandleSPA(w, r) }
func HandleWiFi(w http.ResponseWriter, r *http.Request)       { HandleSPA(w, r) }
func HandleHistory(w http.ResponseWriter, r *http.Request)    { HandleSPA(w, r) }
func HandleGallery(w http.ResponseWriter, r *http.Request)    { HandleSPA(w, r) }
func HandleConfig(w http.ResponseWriter, r *http.Request)     { HandleSPA(w, r) }

func HandlePagePartial(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func HandleBootstrapCSS(w http.ResponseWriter, r *http.Request)     { HandleStatic(w, r) }
func HandleBootstrapJS(w http.ResponseWriter, r *http.Request)      { HandleStatic(w, r) }
func HandleFontAwesomeCSS(w http.ResponseWriter, r *http.Request)   { HandleStatic(w, r) }
func HandleWebfonts(w http.ResponseWriter, r *http.Request)        { HandleStatic(w, r) }
func HandleHtmxJS(w http.ResponseWriter, r *http.Request)         { HandleStatic(w, r) }
func HandleUtilsJS(w http.ResponseWriter, r *http.Request)        { HandleStatic(w, r) }
func HandleComponentsJS(w http.ResponseWriter, r *http.Request)    { HandleStatic(w, r) }
func HandleRouterJS(w http.ResponseWriter, r *http.Request)       { HandleStatic(w, r) }
func HandleThemeJS(w http.ResponseWriter, r *http.Request)        { HandleStatic(w, r) }
func HandleKeyboardJS(w http.ResponseWriter, r *http.Request)     { HandleStatic(w, r) }
func HandleSearchJS(w http.ResponseWriter, r *http.Request)       { HandleStatic(w, r) }
func HandleTypesJS(w http.ResponseWriter, r *http.Request)        { HandleStatic(w, r) }
func HandleStatusControllerJS(w http.ResponseWriter, r *http.Request) { HandleStatic(w, r) }
func HandleWiFiControllerJS(w http.ResponseWriter, r *http.Request) { HandleStatic(w, r) }
func HandleConfigControllerJS(w http.ResponseWriter, r *http.Request) {
	HandleStatic(w, r)
}
func HandleHistoryControllerJS(w http.ResponseWriter, r *http.Request) {
	HandleStatic(w, r)
}
func HandleGalleryControllerJS(w http.ResponseWriter, r *http.Request) {
	HandleStatic(w, r)
}
func HandleStoreJS(w http.ResponseWriter, r *http.Request) { HandleStatic(w, r) }
func HandleManifestJSON(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = "/static/manifest.json"
	HandleStatic(w, r)
}
func HandleServiceWorkerJS(w http.ResponseWriter, r *http.Request) { HandleStatic(w, r) }
func HandleIcon(w http.ResponseWriter, r *http.Request)         { HandleStatic(w, r) }
