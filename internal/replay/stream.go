package replay

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// StreamParams controls playback behaviour.
type StreamParams struct {
	FromIndex int
	Speed     float64 // 1.0 = real-time; 2.0 = double speed; 0 defaults to 1.0
}

// maxDelay is the longest we'll sleep between events regardless of actual gap.
const maxDelay = 5 * time.Second

// Stream writes events[params.FromIndex:] as SSE to w with inter-event timing
// scaled by params.Speed. It returns when all events are sent, the client
// disconnects (r.Context() cancelled), or an error occurs.
func Stream(w http.ResponseWriter, r *http.Request, events []Event, params StreamParams) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	speed := params.Speed
	if speed <= 0 {
		speed = 1.0
	}

	from := params.FromIndex
	if from < 0 {
		from = 0
	}
	if from > len(events) {
		from = len(events)
	}

	slice := events[from:]
	ctx := r.Context()

	for i, ev := range slice {
		// Respect client disconnect.
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Sleep for scaled gap between this event and the previous one.
		if i > 0 {
			prev := slice[i-1]
			if !prev.Timestamp.IsZero() && !ev.Timestamp.IsZero() {
				gap := ev.Timestamp.Sub(prev.Timestamp)
				if gap > 0 {
					delay := time.Duration(float64(gap) / speed)
					if delay > maxDelay {
						delay = maxDelay
					}
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}
		}

		payload := struct {
			Message Event `json:"message"`
			Index   int   `json:"index"`
		}{Message: ev, Index: ev.Index}
		data, err := json.Marshal(payload)
		if err != nil {
			log.Printf("replay: marshal error for event %d: %v", ev.Index, err)
			continue
		}
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		flusher.Flush()
	}

	// total reports the full session size so the client can update its scrubber max.
	fmt.Fprintf(w, "event: done\ndata: {\"total\":%d}\n\n", len(events))
	flusher.Flush()
}
