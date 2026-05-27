package handlers

import (
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
)

func (ctx *Context) HandleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	httputil.JSON(w, http.StatusOK, ctx.VersionInfo())
}
