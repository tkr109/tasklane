package jobs

import (
	"context"
	"encoding/json"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestParseEmail(t *testing.T) {
	e, err := parseEmail(json.RawMessage(`{"to": "Ada <ada@example.com>", "subject": "Hi", "text": "Hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.to.Address != "ada@example.com" || e.subject != "Hi" || e.text != "Hello" {
		t.Fatalf("email = %+v", e)
	}

	e, err = parseEmail(json.RawMessage(`{"to": "bob@example.com", "template": "receipt",
		"data": {"name": "Bob", "amount": "$12.00", "number": "R-1001"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.subject != "Your receipt R-1001" || !strings.Contains(e.text, "payment of $12.00") {
		t.Fatalf("receipt = %+v", e)
	}

	e, err = parseEmail(json.RawMessage(`{"to": "bob@example.com", "template": "welcome",
		"subject": "Hello there", "data": {"name": "Bob"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.subject != "Hello there" || !strings.HasPrefix(e.text, "Hi Bob,") {
		t.Fatalf("a subject in the payload should win over the template's: %+v", e)
	}
}

func TestEmailBadPayloadIsPermanent(t *testing.T) {
	send := EmailSend("127.0.0.1:1", "Tasklane <noreply@tasklane.local>")
	for _, payload := range []string{
		`{"subject": "Hi", "text": "Hello"}`,
		`{"to": "not an address", "subject": "Hi", "text": "Hello"}`,
		`{"to": "ada@example.com", "text": "Hello"}`,
		`{"to": "ada@example.com", "subject": "Hi"}`,
		`{"to": "ada@example.com", "subject": "Hi\r\nBcc: everyone@example.com", "text": "Hello"}`,
		`{"to": "ada@example.com", "template": "invoice"}`,
		`{"to": "ada@example.com", "template": "welcome"}`,
		`{"to": "ada@example.com", "template": "welcome", "data": {"name": 7}}`,
	} {
		if _, err := send(context.Background(), json.RawMessage(payload), noProgress); !IsPermanent(err) {
			t.Errorf("%s: err = %v, want permanent", payload, err)
		}
	}
}

func TestEmailUnreachableServerRetries(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	send := EmailSend(addr, "Tasklane <noreply@tasklane.local>")
	_, err = send(context.Background(), json.RawMessage(`{"to": "ada@example.com", "subject": "Hi", "text": "Hello"}`), noProgress)
	if err == nil || IsPermanent(err) {
		t.Fatalf("err = %v, want a retryable error", err)
	}
}

func TestEmailMessage(t *testing.T) {
	from := &mail.Address{Name: "Tasklane", Address: "noreply@tasklane.local"}
	e := email{to: &mail.Address{Address: "ada@example.com"}, subject: "Café ☕", text: "Grüße\n"}
	msg := string(e.message(from, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))

	for _, want := range []string{
		"From: \"Tasklane\" <noreply@tasklane.local>\r\n",
		"To: <ada@example.com>\r\n",
		"Subject: =?utf-8?q?Caf=C3=A9_=E2=98=95?=\r\n",
		"Date: Fri, 02 Jan 2026 03:04:05 +0000\r\n",
		"\r\n\r\nGr=C3=BC=C3=9Fe\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}
