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

// HandleStaticCSS serves static CSS files
func HandleStaticCSS(w http.ResponseWriter, r *http.Request) {
	// Read the CSS file from embedded filesystem
	content, err := staticFS.ReadFile("static/css/main.css")
	if err != nil {
		http.Error(w, "CSS file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000") // Cache for 1 year
	w.Write(content)
}

// HandleBootstrapCSS serves Bootstrap CSS
func HandleBootstrapCSS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/bootstrap/css/bootstrap.min.css")
	if err != nil {
		http.Error(w, "Bootstrap CSS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Write(content)
}

// HandleThemeCSS serves custom theme CSS
func HandleThemeCSS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/css/theme.css")
	if err != nil {
		http.Error(w, "Theme CSS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Write(content)
}

// HandleBootstrapJS serves Bootstrap JavaScript bundle
func HandleBootstrapJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/bootstrap/js/bootstrap.bundle.min.js")
	if err != nil {
		http.Error(w, "Bootstrap JS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Write(content)
}

// HandleUtilsJS serves utility JavaScript
func HandleUtilsJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/utils.js")
	if err != nil {
		http.Error(w, "Utils JS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Write(content)
}