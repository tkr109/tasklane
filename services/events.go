package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tkr109/tasklane/repositories"
)

const (
	// replaySize is how many recent events a process keeps for streams that
	// reconnect with a Last-Event-ID.
	replaySize = 2048
	// streamBuffer is how far a stream may fall behind before it is dropped. The
	// browser then reconnects and catches up from the replay buffer.
	streamBuffer = 512
	// maxPayload keeps a notification well under Postgres's limit of 8000 bytes.
	maxPayload = 4000
)

// Event is one message on the live stream. Data is JSON.
type Event struct {
	ID   string
	Name string
	Data json.RawMessage
}

// envelope is how an event travels through Postgres.
type envelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

func encodeEvent(name string, data any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(envelope{Event: name, Data: raw})
	if err != nil {
		return "", err
	}
	if len(b) > maxPayload {
		return "", fmt.Errorf("%s event is %d bytes, more than the %d a notification may carry", name, len(b), maxPayload)
	}
	return string(b), nil
}

func decodeEvent(payload string) (name string, data json.RawMessage, err error) {
	var e envelope
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		return "", nil, err
	}
	if e.Event == "" || len(e.Data) == 0 || string(e.Data) == "null" {
		return "", nil, errors.New("an event needs a name and data")
	}
	return e.Event, e.Data, nil
}

// Events relays what Postgres announces on the events channel to this process's
// live streams. Every api listens, so a browser sees every event whichever api it
// is connected to. Event ids are this process's own: a stream that reconnects
// here can pick up where it left off, one that lands elsewhere is told to refetch.
type Events struct {
	repo      *repositories.Events
	process   string
	listening atomic.Bool

	mu       sync.Mutex
	seq      uint64
	recent   []Event
	streams  map[*Stream]struct{}
	watchers []func(Event)
	closed   bool
}

func NewEvents(repo *repositories.Events) *Events {
	b := make([]byte, 4)
	rand.Read(b)
	return &Events{
		repo:    repo,
		process: hex.EncodeToString(b),
		recent:  make([]Event, replaySize),
		streams: map[*Stream]struct{}{},
	}
}

// Publish announces an event to the live streams of every api.
func (e *Events) Publish(ctx context.Context, name string, data any) error {
	payload, err := encodeEvent(name, data)
	if err != nil {
		return err
	}
	return e.repo.Notify(ctx, payload)
}

// Run relays events until ctx ends. When the listening connection has to be
// replaced, events may have been lost in between, so streams are told to refetch.
func (e *Events) Run(ctx context.Context) {
	first := true
	for {
		err := e.repo.Listen(ctx, func() {
			e.listening.Store(true)
			if !first {
				e.deliver("reset", json.RawMessage(`{}`))
			}
			first = false
		}, e.receive)
		e.listening.Store(false)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("listen for events, retrying", "err", err)
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// Listening reports whether events are reaching this process.
func (e *Events) Listening() bool {
	return e.listening.Load()
}

func (e *Events) receive(payload string) {
	name, data, err := decodeEvent(payload)
	if err != nil {
		slog.Warn("drop a malformed event", "err", err)
		return
	}
	e.deliver(name, data)
}

// deliver numbers an event, keeps it for replays and hands it to every stream.
func (e *Events) deliver(name string, data json.RawMessage) {
	e.mu.Lock()
	e.seq++
	ev := Event{ID: e.id(e.seq), Name: name, Data: data}
	e.recent[e.seq%replaySize] = ev
	for s := range e.streams {
		select {
		case s.c <- ev:
		default:
			// Closing makes the browser reconnect and replay what it missed.
			e.drop(s)
		}
	}
	watchers := e.watchers
	e.mu.Unlock()

	for _, watch := range watchers {
		watch(ev)
	}
}

// watch has fn see every event this process relays. fn runs on the listener's
// goroutine, so it must be quick.
func (e *Events) watch(fn func(Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.watchers = append(e.watchers, fn)
}

func (e *Events) id(seq uint64) string {
	return e.process + "-" + strconv.FormatUint(seq, 10)
}

// Stream is one browser's subscription. C closes when the stream should end: the
// process is shutting down, or the browser fell too far behind.
type Stream struct {
	C <-chan Event
	// Backlog is what the browser missed since its Last-Event-ID, oldest first. It
	// is a single reset event when this process can't tell.
	Backlog []Event

	c      chan Event
	events *Events
}

// Subscribe starts a stream. lastEventID is the last event the browser saw, if it
// is reconnecting.
func (e *Events) Subscribe(lastEventID string) *Stream {
	c := make(chan Event, streamBuffer)
	s := &Stream{C: c, c: c, events: e}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		close(c)
		return s
	}
	if lastEventID != "" {
		s.Backlog = e.since(lastEventID)
	}
	e.streams[s] = struct{}{}
	return s
}

// since returns the kept events after the one with the given id.
func (e *Events) since(id string) []Event {
	process, n, _ := strings.Cut(id, "-")
	seq, err := strconv.ParseUint(n, 10, 64)
	kept := min(e.seq, replaySize)
	if process != e.process || err != nil || seq > e.seq || seq < e.seq-kept {
		return []Event{{ID: e.id(e.seq), Name: "reset", Data: json.RawMessage(`{}`)}}
	}
	missed := make([]Event, 0, e.seq-seq)
	for s := seq + 1; s <= e.seq; s++ {
		missed = append(missed, e.recent[s%replaySize])
	}
	return missed
}

func (s *Stream) Close() {
	s.events.mu.Lock()
	defer s.events.mu.Unlock()
	s.events.drop(s)
}

// drop ends a stream. The caller holds e.mu.
func (e *Events) drop(s *Stream) {
	if _, ok := e.streams[s]; ok {
		delete(e.streams, s)
		close(s.c)
	}
}

// Close ends every stream. Server shutdown calls it, as open streams never go idle.
func (e *Events) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for s := range e.streams {
		e.drop(s)
	}
}
