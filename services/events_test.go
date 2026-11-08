package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeEventRoundTrips(t *testing.T) {
	type worker struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	payload, err := encodeEvent("worker", worker{ID: "w-1", State: "draining"})
	if err != nil {
		t.Fatal(err)
	}
	if payload != `{"event":"worker","data":{"id":"w-1","state":"draining"}}` {
		t.Fatalf("payload = %s", payload)
	}
	name, data, err := decodeEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got worker
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if name != "worker" || got.ID != "w-1" || got.State != "draining" {
		t.Fatalf("decoded %q %+v", name, got)
	}

	// The jobs trigger builds its payload in SQL, with jsonb's spacing.
	name, data, err = decodeEvent(`{"data": {"id": 7, "status": "running"}, "event": "job"}`)
	if err != nil || name != "job" || string(data) != `{"id": 7, "status": "running"}` {
		t.Fatalf("trigger payload: %q %s %v", name, data, err)
	}
}

func TestEncodeEventRefusesWhatPostgresWouldReject(t *testing.T) {
	big := map[string]string{"error": strings.Repeat("x", maxPayload)}
	if _, err := encodeEvent("job", big); err == nil {
		t.Fatal("an event over the payload limit was encoded")
	}
}

func TestDecodeEventRejectsMalformedPayloads(t *testing.T) {
	for _, bad := range []string{"", "nope", "{}", `{"event":"job"}`, `{"data":{}}`, `{"event":"job","data":null}`} {
		if _, _, err := decodeEvent(bad); err == nil {
			t.Errorf("decodeEvent(%q) should fail", bad)
		}
	}
}

func deliverN(e *Events, n int) {
	for range n {
		e.deliver("job", json.RawMessage(`{}`))
	}
}

func TestStreamsReplayWhatTheyMissed(t *testing.T) {
	e := NewEvents(nil)
	deliverN(e, 5)
	second := e.id(2)

	s := e.Subscribe(second)
	defer s.Close()
	if len(s.Backlog) != 3 || s.Backlog[0].ID != e.id(3) || s.Backlog[2].ID != e.id(5) {
		t.Fatalf("backlog after event 2: %+v", s.Backlog)
	}
	if up := e.Subscribe(e.id(5)); len(up.Backlog) != 0 {
		t.Fatalf("a stream that missed nothing got %d events", len(up.Backlog))
	}

	e.deliver("worker", json.RawMessage(`{"id":"w-1"}`))
	if ev := <-s.C; ev.ID != e.id(6) || ev.Name != "worker" {
		t.Fatalf("live event: %+v", ev)
	}
}

func TestStreamsThatCantReplayAreReset(t *testing.T) {
	e := NewEvents(nil)
	deliverN(e, replaySize+10)

	for name, id := range map[string]string{
		"another process":  "0badc0de-3",
		"too far back":     e.id(5),
		"not an id at all": "garbage",
		"from the future":  e.id(replaySize + 99),
	} {
		s := e.Subscribe(id)
		if len(s.Backlog) != 1 || s.Backlog[0].Name != "reset" || s.Backlog[0].ID != e.id(replaySize+10) {
			t.Errorf("%s: backlog %+v, want one reset", name, s.Backlog)
		}
		s.Close()
	}
	if s := e.Subscribe(e.id(10)); len(s.Backlog) != replaySize {
		t.Errorf("the oldest kept event: replayed %d, want %d", len(s.Backlog), replaySize)
	}
}

func TestASlowStreamIsDropped(t *testing.T) {
	e := NewEvents(nil)
	slow := e.Subscribe("")
	fast := e.Subscribe("")
	defer fast.Close()

	for range streamBuffer + 1 {
		e.deliver("job", json.RawMessage(`{}`))
		<-fast.C
	}
	n := 0
	for range slow.C {
		n++
	}
	if n != streamBuffer {
		t.Fatalf("the slow stream got %d events before it closed, want %d", n, streamBuffer)
	}
	slow.Close() // closing twice is fine

	e.Close()
	if _, open := <-fast.C; open {
		t.Fatal("a stream outlived Close")
	}
	if _, open := <-e.Subscribe("").C; open {
		t.Fatal("a stream opened after Close")
	}
}
