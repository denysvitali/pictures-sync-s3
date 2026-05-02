package handlers

import (
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
)

func (ctx *Context) HandleOTAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.MethodNotAllowed(w)
		return
	}
	if ctx.OTAMgr == nil {
		httputil.ServiceUnavailable(w, "OTA manager not initialized")
		return
	}
	httputil.JSON(w, http.StatusOK, ctx.OTAMgr.Status())
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
	status, err := ctx.OTAMgr.Start(r.Context())
	if err != nil {
		httputil.Error(w, http.StatusConflict, err.Error())
		return
	}
	httputil.JSON(w, http.StatusAccepted, status)
}
