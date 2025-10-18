package webui

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/css/*.css static/bootstrap/css/*.css static/bootstrap/js/*.js static/js/*.js
var staticFS embed.FS

var templates *template.Template

// PageData represents the data passed to templates
type PageData struct {
	Title      string
	ActivePage string
}

// init loads and parses all templates
func init() {
	var err error
	templates, err = template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		panic("Failed to parse templates: " + err.Error())
	}
}

// HandleIndex serves the status page (default)
func HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := PageData{
		Title:      "Status",
		ActivePage: "status",
	}

	w.Header().Set("Content-Type", "text/html")
	err := templates.ExecuteTemplate(w, "status_standalone.html", data)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleWiFi serves the WiFi management page
func HandleWiFi(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:      "WiFi Management",
		ActivePage: "wifi",
	}

	w.Header().Set("Content-Type", "text/html")
	err := templates.ExecuteTemplate(w, "wifi_page.html", data)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleHistory serves the sync history page
func HandleHistory(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:      "Sync History",
		ActivePage: "history",
	}

	w.Header().Set("Content-Type", "text/html")
	err := templates.ExecuteTemplate(w, "history_standalone.html", data)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleGallery serves the photo gallery page
func HandleGallery(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:      "Photo Gallery",
		ActivePage: "gallery",
	}

	w.Header().Set("Content-Type", "text/html")
	err := templates.ExecuteTemplate(w, "gallery_standalone.html", data)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleConfig serves the configuration page
func HandleConfig(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:      "Configuration",
		ActivePage: "config",
	}

	w.Header().Set("Content-Type", "text/html")
	err := templates.ExecuteTemplate(w, "config_standalone.html", data)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleStaticCSS serves static CSS files (legacy)
func HandleStaticCSS(w http.ResponseWriter, r *http.Request) {
	// Read the CSS file from embedded filesystem
	content, err := staticFS.ReadFile("static/css/main.css")
	if err != nil {
		http.Error(w, "CSS file not found", http.StatusNotFound)
		return
	}

	// Moderate caching for legacy CSS
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24 hours
	w.Header().Set("ETag", `"main-css-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"main-css-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(content)
}

// HandleBootstrapCSS serves Bootstrap CSS with aggressive caching
func HandleBootstrapCSS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/bootstrap/css/bootstrap.min.css")
	if err != nil {
		http.Error(w, "Bootstrap CSS not found", http.StatusNotFound)
		return
	}

	// Set aggressive caching headers for immutable Bootstrap assets
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"bootstrap-5.3.8-css"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"bootstrap-5.3.8-css"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(content)
}

// HandleThemeCSS serves custom theme CSS with moderate caching
func HandleThemeCSS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/css/theme.css")
	if err != nil {
		http.Error(w, "Theme CSS not found", http.StatusNotFound)
		return
	}

	// Moderate caching for custom assets that may change
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24 hours
	w.Header().Set("ETag", `"theme-css-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"theme-css-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(content)
}

// HandleBootstrapJS serves Bootstrap JavaScript bundle with aggressive caching
func HandleBootstrapJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/bootstrap/js/bootstrap.bundle.min.js")
	if err != nil {
		http.Error(w, "Bootstrap JS not found", http.StatusNotFound)
		return
	}

	// Set aggressive caching headers for immutable Bootstrap assets
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"bootstrap-5.3.8-js"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"bootstrap-5.3.8-js"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(content)
}

// HandleUtilsJS serves utility JavaScript with moderate caching
func HandleUtilsJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/utils.js")
	if err != nil {
		http.Error(w, "Utils JS not found", http.StatusNotFound)
		return
	}

	// Moderate caching for custom assets that may change
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24 hours
	w.Header().Set("ETag", `"utils-js-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"utils-js-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(content)
}