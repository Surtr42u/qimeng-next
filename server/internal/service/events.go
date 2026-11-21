package service

import (
	"errors"
	"log/slog"

	"qimeng.local/next/server/internal/events"
)

// change is one invalidation recorded by a write. Only the topic and an
// optional asset identity are kept; the payload is derived in one place so
// every publisher uses the same wire shape.
type change struct {
	topic   string
	assetID string
}

// changed records one invalidation for this write. Duplicates are collapsed so
// a write that touches the same asset twice still produces one frame.
func (w *Write) changed(topic, assetID string) {
	key := topic + "\x00" + assetID
	if w.changeKeys == nil {
		w.changeKeys = map[string]struct{}{}
	}
	if _, seen := w.changeKeys[key]; seen {
		return
	}
	w.changeKeys[key] = struct{}{}
	w.changes = append(w.changes, change{topic: topic, assetID: assetID})
}

// libraryChanged says the client's library view may be stale: assets, authors,
// vocabulary, collections, trash or libraries changed. It is deliberately a
// broadcast signal; clients re-read whatever they show.
func (w *Write) libraryChanged() { w.changed(events.TopicLibraryChanged, "") }

// uploadDone names the asset that an upload just published. It supplements
// library.changed for clients that can refresh one entry precisely.
func (w *Write) uploadDone(assetID string) { w.changed(events.TopicUploadDone, assetID) }

// publishChanges emits recorded invalidations in record order.
func (s *Service) publishChanges(changes []change) {
	for _, c := range changes {
		s.publish(c)
	}
}

// publish is the only writer to the bus. Notification is best effort: a closed
// bus during shutdown or a slow subscriber must never fail a committed write.
func (s *Service) publish(c change) {
	if s.Events == nil {
		return
	}
	event := events.Event{Topic: c.topic}
	if c.assetID != "" {
		event.Payload = events.AssetChangedEvent{AssetID: c.assetID}
	}
	if err := s.Events.Publish(event); err != nil && !errors.Is(err, events.ErrBusClosed) {
		// Log the topic only; payloads and principal data stay out of logs.
		slog.Warn("event publish failed", "topic", c.topic)
	}
}

// CloseEvents ends active streams. The process calls it before HTTP shutdown
// so idle long-lived connections do not hold the drain open; Close repeats it.
func (s *Service) CloseEvents() {
	if s.Events != nil {
		s.Events.Close()
	}
}
