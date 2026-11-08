package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestEmailArrivesInMailpit(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	mailpit, err := testcontainers.Run(ctx, "axllent/mailpit:v1.27.0",
		testcontainers.WithExposedPorts("1025/tcp", "8025/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("1025/tcp"),
			wait.ForHTTP("/livez").WithPort("8025/tcp"),
		),
	)
	testcontainers.CleanupContainer(t, mailpit)
	if err != nil {
		t.Fatal(err)
	}
	smtpAddr, err := mailpit.PortEndpoint(ctx, "1025/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	api, err := mailpit.PortEndpoint(ctx, "8025/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}

	send := EmailSend(smtpAddr, "Tasklane <noreply@tasklane.local>")
	payload := json.RawMessage(`{"to": "Ada <ada@example.com>", "template": "report-ready",
		"data": {"name": "Ada", "report": "Q3 usage", "url": "https://example.com/r/42"}}`)
	got, err := send(ctx, payload, noProgress)
	if err != nil {
		t.Fatal(err)
	}
	if got != (emailResult{To: "ada@example.com", Subject: "Your report Q3 usage is ready"}) {
		t.Errorf("result = %+v", got)
	}

	var list struct {
		Messages []struct {
			ID      string
			Subject string
			To      []struct{ Address string }
		}
	}
	getJSON(t, api+"/api/v1/messages", &list)
	if len(list.Messages) != 1 {
		t.Fatalf("mailpit has %d messages, want 1", len(list.Messages))
	}
	m := list.Messages[0]
	if m.Subject != "Your report Q3 usage is ready" || len(m.To) != 1 || m.To[0].Address != "ada@example.com" {
		t.Fatalf("message = %+v", m)
	}

	var full struct{ Text string }
	getJSON(t, api+"/api/v1/message/"+m.ID, &full)
	if !strings.Contains(full.Text, "https://example.com/r/42") {
		t.Errorf("text = %q", full.Text)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}
