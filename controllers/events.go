package controllers

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tkr109/tasklane/services"
)

// heartbeatEvery keeps proxies from closing a quiet stream.
const heartbeatEvery = 15 * time.Second

// Events serves the dashboard's live stream.
type Events struct {
	events *services.Events
}

func NewEvents(events *services.Events) *Events {
	return &Events{events: events}
}

// Stream sends live events as server-sent events. A browser that reconnects sends
// Last-Event-ID and gets what it missed, or a reset event telling it to refetch
// when this process can't replay that far back.
func (h *Events) Stream(w http.ResponseWriter, r *http.Request) {
	stream := h.events.Subscribe(r.Header.Get("Last-Event-ID"))
	defer stream.Close()

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	for _, ev := range stream.Backlog {
		writeEvent(w, ev)
	}
	if err := rc.Flush(); err != nil {
		return
	}

	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-stream.C:
			if !ok {
				return
			}
			writeEvent(w, ev)
		case <-beat.C:
			io.WriteString(w, ": ping\n\n")
		}
		// A failed write shows up here too.
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func writeEvent(w io.Writer, ev services.Event) {
	fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Name, ev.Data)
}
