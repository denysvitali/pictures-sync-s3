package handlers

import (
	"errors"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
)

type daemonStatusMapping struct {
	code   string
	status int
}

var (
	noSDCardBadRequest   = daemonStatusMapping{code: daemoncontrol.CodeNoSDCardMounted, status: http.StatusBadRequest}
	invalidDeviceRequest = daemonStatusMapping{code: daemoncontrol.CodeInvalidDevice, status: http.StatusBadRequest}
	syncActiveConflict   = daemonStatusMapping{code: daemoncontrol.CodeSyncAlreadyActive, status: http.StatusConflict}
	syncActiveBadRequest = daemonStatusMapping{code: daemoncontrol.CodeSyncAlreadyActive, status: http.StatusBadRequest}
)

func writeDaemonCommandError(w http.ResponseWriter, err error, mappings ...daemonStatusMapping) {
	http.Error(w, err.Error(), daemonCommandStatus(err, mappings...))
}

func daemonCommandStatus(err error, mappings ...daemonStatusMapping) int {
	var commandErr *daemoncontrol.CommandError
	if errors.As(err, &commandErr) {
		for _, mapping := range mappings {
			if mapping.code == commandErr.Code {
				return mapping.status
			}
		}
	}
	return http.StatusServiceUnavailable
}

func daemonErrorMessage(err error) string {
	var commandErr *daemoncontrol.CommandError
	if errors.As(err, &commandErr) && commandErr.Message != "" {
		return commandErr.Message
	}
	return err.Error()
}

func daemonHTTPStatus(err error) int {
	message := daemonErrorMessage(err)
	switch message {
	case "access denied":
		return http.StatusForbidden
	case "unsupported file type", "path parameter required", "no SD card mounted":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
