package webui

import (
	"compress/gzip"
	"embed"
	"html/template"
	"io"
	"net/http"
	"strings"
)

//go:embed templates/*.html templates/components/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/css/theme.css static/bootstrap/css/*.css static/bootstrap/js/*.js static/js/*.js static/fontawesome/*.css static/webfonts/* static/manifest.json static/sw.js
var staticFS embed.FS

var templates *template.Template
var partials map[string][]byte

// PageData represents the data passed to templates
type PageData struct {
	Title      string
	ActivePage string
}

// init loads and parses all templates
func init() {
	var err error
	// Parse all templates including layout and components
	templates, err = template.ParseFS(templatesFS, "templates/*.html", "templates/components/*.html")
	if err != nil {
		panic("Failed to parse templates: " + err.Error())
	}

	// Load partials into memory for fast serving
	partials = make(map[string][]byte)
	partialFiles := []string{"status", "history", "wifi", "gallery", "config"}
	for _, name := range partialFiles {
		content, err := templatesFS.ReadFile("templates/partials/" + name + ".html")
		if err != nil {
			panic("Failed to load partial " + name + ": " + err.Error())
		}
		partials[name] = content
	}
}

// renderPage is a helper to render a page with the layout
func renderPage(w http.ResponseWriter, page string, data PageData) {
	w.Header().Set("Content-Type", "text/html")

	// Execute the page template, which includes the layout
	// The page template (e.g., status.html) starts with {{template "layout" .}}
	// and defines the "content" and "scripts" blocks
	err := templates.ExecuteTemplate(w, page+".html", data)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleIndex serves the status page (default)
func HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	renderPage(w, "status", PageData{
		Title:      "Status",
		ActivePage: "status",
	})
}

// HandleWiFi serves the WiFi management page
func HandleWiFi(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "wifi", PageData{
		Title:      "WiFi Management",
		ActivePage: "wifi",
	})
}

// HandleHistory serves the sync history page
func HandleHistory(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "history", PageData{
		Title:      "Sync History",
		ActivePage: "history",
	})
}

// HandleGallery serves the photo gallery page
func HandleGallery(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "gallery", PageData{
		Title:      "Photo Gallery",
		ActivePage: "gallery",
	})
}

// HandleConfig serves the configuration page
func HandleConfig(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "config", PageData{
		Title:      "Configuration",
		ActivePage: "config",
	})
}

// HandleSPA serves the single-page application
func HandleSPA(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	content, err := templatesFS.ReadFile("templates/spa.html")
	if err != nil {
		http.Error(w, "SPA template not found", http.StatusInternalServerError)
		return
	}
	w.Write(content)
}

// HandlePagePartial serves a page partial for htmx requests
func HandlePagePartial(w http.ResponseWriter, r *http.Request) {
	// Extract page name from path (/api/pages/{page})
	page := r.URL.Path[len("/api/pages/"):]

	// Validate page name
	if page == "" {
		http.Error(w, "Page not specified", http.StatusBadRequest)
		return
	}

	// Get partial content
	content, ok := partials[page]
	if !ok {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	// Serve partial HTML
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
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

	serveWithGzip(w, r, content)
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
	w.Header().Set("ETag", `"theme-css-v2"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"theme-css-v2"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// gzipResponseWriter wraps http.ResponseWriter to provide gzip compression
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// serveWithGzip serves content with gzip compression if client accepts it
func serveWithGzip(w http.ResponseWriter, r *http.Request, content []byte) {
	// Check if client accepts gzip
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Write(content)
		return
	}

	// Add gzip encoding header
	w.Header().Set("Content-Encoding", "gzip")

	// Create gzip writer
	gz := gzip.NewWriter(w)
	defer gz.Close()

	gz.Write(content)
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

	serveWithGzip(w, r, content)
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

	serveWithGzip(w, r, content)
}

// HandleComponentsJS serves components JavaScript with moderate caching
func HandleComponentsJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/components.js")
	if err != nil {
		http.Error(w, "Components JS not found", http.StatusNotFound)
		return
	}

	// Moderate caching for custom assets that may change
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24 hours
	w.Header().Set("ETag", `"components-js-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"components-js-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleHtmxJS serves htmx library with aggressive caching
func HandleHtmxJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/htmx.min.js")
	if err != nil {
		http.Error(w, "htmx JS not found", http.StatusNotFound)
		return
	}

	// Aggressive caching for immutable htmx library
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"htmx-2.0.3-js"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"htmx-2.0.3-js"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleRouterJS serves router JavaScript with moderate caching
func HandleRouterJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/router.js")
	if err != nil {
		http.Error(w, "Router JS not found", http.StatusNotFound)
		return
	}

	// Moderate caching for custom assets that may change
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24 hours
	w.Header().Set("ETag", `"router-js-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"router-js-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleThemeJS serves theme manager JavaScript with moderate caching
func HandleThemeJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/theme.js")
	if err != nil {
		http.Error(w, "Theme JS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"theme-js-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	if r.Header.Get("If-None-Match") == `"theme-js-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleKeyboardJS serves keyboard shortcuts JavaScript with moderate caching
func HandleKeyboardJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/keyboard.js")
	if err != nil {
		http.Error(w, "Keyboard JS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"keyboard-js-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	if r.Header.Get("If-None-Match") == `"keyboard-js-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleSearchJS serves search functionality JavaScript with moderate caching
func HandleSearchJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/js/search.js")
	if err != nil {
		http.Error(w, "Search JS not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"search-js-v1"`)
	w.Header().Set("Vary", "Accept-Encoding")

	if r.Header.Get("If-None-Match") == `"search-js-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleManifestJSON serves PWA manifest with moderate caching
func HandleManifestJSON(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/manifest.json")
	if err != nil {
		http.Error(w, "Manifest not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(content)
}

// HandleServiceWorkerJS serves service worker with no caching (must always be fresh)
func HandleServiceWorkerJS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		http.Error(w, "Service worker not found", http.StatusNotFound)
		return
	}

	// Service workers must not be cached to ensure updates are applied
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(content)
}

// HandleIcon serves a dynamically generated SVG icon
func HandleIcon(w http.ResponseWriter, r *http.Request) {
	// Extract size from path (icon-192.png -> 192)
	size := "192"
	if strings.Contains(r.URL.Path, "512") {
		size = "512"
	}

	// Generate a simple SVG icon with camera emoji
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + size + ` ` + size + `">
		<rect width="` + size + `" height="` + size + `" fill="#6366f1"/>
		<text x="50%" y="50%" text-anchor="middle" dy=".35em" font-size="` + size + `px" font-family="sans-serif">📸</text>
	</svg>`

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(svg))
}
// HandleFontAwesomeCSS serves Font Awesome CSS with aggressive caching
func HandleFontAwesomeCSS(w http.ResponseWriter, r *http.Request) {
	content, err := staticFS.ReadFile("static/fontawesome/all.min.css")
	if err != nil {
		http.Error(w, "Font Awesome CSS not found", http.StatusNotFound)
		return
	}

	// Set aggressive caching headers for immutable Font Awesome assets
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"fontawesome-6.5.1-css"`)
	w.Header().Set("Vary", "Accept-Encoding")

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == `"fontawesome-6.5.1-css"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	serveWithGzip(w, r, content)
}

// HandleWebfonts serves Font Awesome webfonts with aggressive caching
func HandleWebfonts(w http.ResponseWriter, r *http.Request) {
	// Extract font file name from URL (/static/webfonts/fa-solid-900.woff2)
	fontFile := r.URL.Path[len("/static/"):]
	
	content, err := staticFS.ReadFile(fontFile)
	if err != nil {
		http.Error(w, "Font not found", http.StatusNotFound)
		return
	}

	// Determine content type based on extension
	contentType := "application/octet-stream"
	if strings.HasSuffix(fontFile, ".woff2") {
		contentType = "font/woff2"
	} else if strings.HasSuffix(fontFile, ".woff") {
		contentType = "font/woff"
	} else if strings.HasSuffix(fontFile, ".ttf") {
		contentType = "font/ttf"
	}

	// Set aggressive caching headers for immutable font files
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Access-Control-Allow-Origin", "*") // Allow fonts to be used cross-origin
	
	w.Write(content)
}
