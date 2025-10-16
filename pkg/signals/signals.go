package signals

import (
	"os"
	"os/signal"
	"syscall"
)

// Handler manages OS signal handling
type Handler struct {
	sigChan chan os.Signal
}

// NewHandler creates a new signal handler
func NewHandler() *Handler {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	return &Handler{
		sigChan: sigChan,
	}
}

// Wait blocks until a shutdown signal is received
func (h *Handler) Wait() {
	<-h.sigChan
}

// Channel returns the underlying signal channel for use in select statements
func (h *Handler) Channel() <-chan os.Signal {
	return h.sigChan
}

// Stop stops listening for signals
func (h *Handler) Stop() {
	signal.Stop(h.sigChan)
	close(h.sigChan)
}
