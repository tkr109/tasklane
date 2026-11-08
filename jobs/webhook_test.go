package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func noProgress(int) {}

// verify checks a signature header the way a receiver would.
func verify(t *testing.T, secret, header string, body []byte) {
	t.Helper()
	ts, sig, ok := strings.Cut(header, ",")
	if !ok || !strings.HasPrefix(ts, "t=") || !strings.HasPrefix(sig, "v1=") {
		t.Fatalf("malformed signature %q", header)
	}
	unix, err := strconv.ParseInt(strings.TrimPrefix(ts, "t="), 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > time.Minute {
		t.Fatalf("bad timestamp in %q", header)
	}
	got, err := hex.DecodeString(strings.TrimPrefix(sig, "v1="))
	if err != nil {
		t.Fatalf("bad hex in %q", header)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.TrimPrefix(ts, "t=") + "."))
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		t.Fatal("signature does not verify")
	}
}

func TestWebhookSignsTheBody(t *testing.T) {
	const secret = "whsec_test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("got %s with content type %q", r.Method, r.Header.Get("Content-Type"))
		}
		if string(body) != `{"event":"paid"}` {
			t.Errorf("body = %s", body)
		}
		verify(t, secret, r.Header.Get(signatureHeader), body)
		w.Write([]byte("  thanks\n"))
	}))
	defer srv.Close()

	payload := json.RawMessage(`{"url": "` + srv.URL + `", "body": {"event":"paid"}}`)
	got, err := Webhook(secret)(context.Background(), payload, noProgress)
	if err != nil {
		t.Fatal(err)
	}
	if got != (webhookResult{Status: 200, Body: "thanks"}) {
		t.Fatalf("result = %+v", got)
	}
}

func TestWebhookAnswers(t *testing.T) {
	tests := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusNotFound, true},
		{http.StatusGone, true},
		{http.StatusFound, true},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusServiceUnavailable, false},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
		}))
		payload := json.RawMessage(`{"url": "` + srv.URL + `"}`)
		_, err := Webhook("s")(context.Background(), payload, noProgress)
		srv.Close()

		if err == nil {
			t.Errorf("%d: no error", tt.status)
			continue
		}
		if IsPermanent(err) != tt.permanent {
			t.Errorf("%d: permanent = %v, want %v (%v)", tt.status, IsPermanent(err), tt.permanent, err)
		}
	}
}

func TestWebhookUnreachableRetries(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	_, err := Webhook("s")(context.Background(), json.RawMessage(`{"url": "`+srv.URL+`"}`), noProgress)
	if err == nil || IsPermanent(err) {
		t.Fatalf("err = %v, want a retryable error", err)
	}
}

func TestWebhookBadPayloadIsPermanent(t *testing.T) {
	for _, payload := range []string{`{}`, `{"url": "ftp://example.com"}`, `{"url": "/relative"}`, `{"url": 5}`} {
		_, err := Webhook("s")(context.Background(), json.RawMessage(payload), noProgress)
		if !IsPermanent(err) {
			t.Errorf("%s: err = %v, want permanent", payload, err)
		}
	}
}
