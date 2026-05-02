package handlers

import "net/http"

func (ctx *Context) HandleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	JSONResponse(w, ctx.VersionInfo())
}
