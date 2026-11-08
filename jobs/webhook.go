package jobs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const signatureHeader = "Tasklane-Signature"

type webhookResult struct {
	Status int    `json:"status"`
	Body   string `json:"body,omitempty"`
}

// Webhook POSTs the payload's body to its url, signed with secret. A 5xx, 408,
// 429 or network error is retried; any other answer outside 2xx is permanent.
func Webhook(secret string) Handler {
	// A redirect would turn the POST into a GET, so it counts as an answer.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	return func(ctx context.Context, payload json.RawMessage, _ Progress) (any, error) {
		var p struct {
			URL  string          `json:"url"`
			Body json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, Permanent(fmt.Errorf("webhook: bad payload: %w", err))
		}
		if u, err := url.Parse(p.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, Permanent(fmt.Errorf("webhook: url must be an http or https address, got %q", p.URL))
		}
		if len(p.Body) == 0 {
			p.Body = json.RawMessage("{}")
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(p.Body))
		if err != nil {
			return nil, Permanent(fmt.Errorf("webhook: %w", err))
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Tasklane-Webhook/1")
		req.Header.Set(signatureHeader, sign([]byte(secret), time.Now(), p.Body))

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("webhook: %w", err)
		}
		defer resp.Body.Close()
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		// Reading the rest lets the connection go back to the pool.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		body := snippet(head)

		code := resp.StatusCode
		switch {
		case code >= 200 && code < 300:
			return webhookResult{Status: code, Body: body}, nil
		case code >= 500 || code == http.StatusTooManyRequests || code == http.StatusRequestTimeout:
			return nil, fmt.Errorf("webhook: got %s: %s", resp.Status, body)
		default:
			return nil, Permanent(fmt.Errorf("webhook: got %s: %s", resp.Status, body))
		}
	}
}

// sign returns a Stripe-style signature header. The receiver recomputes the HMAC
// over "<t>.<body>" and should reject a t that is too old, to stop replays.
func sign(secret []byte, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// snippet makes a piece of a response safe to keep in Postgres, which rejects
// invalid UTF-8 and NUL bytes in text.
func snippet(b []byte) string {
	s := strings.ToValidUTF8(string(b), "")
	return strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
}
