package jobs

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"text/template"
	"time"
)

type emailTemplate struct {
	subject, text *template.Template
}

func newTemplate(subject, text string) emailTemplate {
	return emailTemplate{
		subject: template.Must(template.New("subject").Option("missingkey=error").Parse(subject)),
		text:    template.Must(template.New("text").Option("missingkey=error").Parse(text)),
	}
}

var emailTemplates = map[string]emailTemplate{
	"welcome": newTemplate("Welcome to Tasklane, {{.name}}",
		"Hi {{.name}},\n\nYour team is set up and ready to send jobs.\n\nThe Tasklane team\n"),
	"receipt": newTemplate("Your receipt {{.number}}",
		"Hi {{.name}},\n\nWe received your payment of {{.amount}}. Receipt number: {{.number}}.\n\nThanks!\n"),
	"report-ready": newTemplate("Your report {{.report}} is ready",
		"Hi {{.name}},\n\nYour report {{.report}} has finished. Download it here:\n{{.url}}\n"),
}

type email struct {
	to            *mail.Address
	subject, text string
}

type emailResult struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
}

// EmailSend sends a plain text email through the SMTP server at addr. The
// subject and text come from the payload or from one of the built-in templates.
func EmailSend(addr, from string) Handler {
	return func(ctx context.Context, payload json.RawMessage, _ Progress) (any, error) {
		e, err := parseEmail(payload)
		if err != nil {
			return nil, Permanent(fmt.Errorf("email.send: %w", err))
		}
		sender, err := mail.ParseAddress(from)
		if err != nil {
			return nil, fmt.Errorf("email.send: SMTP_FROM: %w", err)
		}
		if err := sendMail(ctx, addr, sender, e); err != nil {
			// A 5xx reply, such as an unknown mailbox, will be the same next time.
			var reply *textproto.Error
			if errors.As(err, &reply) && reply.Code >= 500 {
				return nil, Permanent(fmt.Errorf("email.send: %w", err))
			}
			return nil, fmt.Errorf("email.send: %w", err)
		}
		return emailResult{To: e.to.Address, Subject: e.subject}, nil
	}
}

func parseEmail(payload json.RawMessage) (email, error) {
	var p struct {
		To       string            `json:"to"`
		Subject  string            `json:"subject"`
		Text     string            `json:"text"`
		Template string            `json:"template"`
		Data     map[string]string `json:"data"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return email{}, fmt.Errorf("bad payload: %w", err)
	}
	to, err := mail.ParseAddress(p.To)
	if err != nil {
		return email{}, fmt.Errorf("to %q is not an email address", p.To)
	}

	e := email{to: to, subject: p.Subject, text: p.Text}
	if p.Template != "" {
		tmpl, ok := emailTemplates[p.Template]
		if !ok {
			return email{}, fmt.Errorf("no template called %q", p.Template)
		}
		subject, text, err := tmpl.render(p.Data)
		if err != nil {
			return email{}, fmt.Errorf("template %s: %w", p.Template, err)
		}
		// Anything the payload spells out wins over the template.
		e.subject = cmp.Or(e.subject, subject)
		e.text = cmp.Or(e.text, text)
	}

	switch {
	case strings.TrimSpace(e.subject) == "":
		return email{}, errors.New("subject is empty")
	case strings.ContainsAny(e.subject, "\r\n"):
		return email{}, errors.New("subject has a line break")
	case strings.TrimSpace(e.text) == "":
		return email{}, errors.New("text is empty")
	}
	return e, nil
}

func (t emailTemplate) render(data map[string]string) (subject, text string, err error) {
	var s, b strings.Builder
	if err := t.subject.Execute(&s, data); err != nil {
		return "", "", err
	}
	if err := t.text.Execute(&b, data); err != nil {
		return "", "", err
	}
	return s.String(), b.String(), nil
}

func (e email) message(from *mail.Address, now time.Time) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", e.to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", e.subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", now.Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	qp := quotedprintable.NewWriter(&buf)
	qp.Write([]byte(e.text))
	qp.Close()
	return buf.Bytes()
}

// sendMail is smtp.SendMail with a context. Mailpit needs no auth or TLS.
func sendMail(ctx context.Context, addr string, from *mail.Address, e email) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	// net/smtp knows nothing of contexts, so a cancelled job cuts the connection.
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })
	defer stop()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(e.to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(e.message(from, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
