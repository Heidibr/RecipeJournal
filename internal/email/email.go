// Package email is a tiny outbound mail abstraction. In dev (no SMTP env
// configured) it logs the message body to stdout so links can be clicked
// straight from the terminal. In prod it sends via plain net/smtp using
// SMTP_HOST / SMTP_PORT / SMTP_USER / SMTP_PASS / SMTP_FROM.
//
// The interface is small on purpose — the verification and password-reset
// flows only need to send a single plain-text body with a clickable link.
package email

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strings"
)

// Sender is what callers depend on. AppURL is the public base URL the sender
// embeds into links, so a caller passes a relative path and gets a full URL.
type Sender interface {
	Send(to, subject, body string) error
	AppURL() string
}

// FromEnv returns an SMTPSender if SMTP_HOST is set, otherwise a LogSender.
// appURL is the public origin (e.g. https://recipes.example.com) embedded
// in emailed links — set APP_URL in prod.
func FromEnv(appURL string) Sender {
	if h := os.Getenv("SMTP_HOST"); h != "" {
		port := os.Getenv("SMTP_PORT")
		if port == "" {
			port = "587"
		}
		return &SMTPSender{
			Host:    h,
			Port:    port,
			User:    os.Getenv("SMTP_USER"),
			Pass:    os.Getenv("SMTP_PASS"),
			From:    os.Getenv("SMTP_FROM"),
			BaseURL: appURL,
		}
	}
	return &LogSender{BaseURL: appURL}
}

// LogSender writes the email to stdout instead of sending it. Useful in dev:
// click the verification/reset link straight out of the terminal.
type LogSender struct {
	BaseURL string
}

func (l *LogSender) AppURL() string { return l.BaseURL }

func (l *LogSender) Send(to, subject, body string) error {
	log.Printf("\n========== EMAIL (dev: not sent) ==========\nTo: %s\nSubject: %s\n\n%s\n===========================================\n",
		to, subject, body)
	return nil
}

// SMTPSender uses net/smtp from the stdlib. Works against Gmail App Passwords,
// Fastmail, Postmark SMTP, Resend SMTP, anything that speaks STARTTLS on 587.
type SMTPSender struct {
	Host    string
	Port    string
	User    string
	Pass    string
	From    string
	BaseURL string
}

func (s *SMTPSender) AppURL() string { return s.BaseURL }

func (s *SMTPSender) Send(to, subject, body string) error {
	if s.From == "" {
		return fmt.Errorf("smtp: SMTP_FROM not set")
	}
	addr := s.Host + ":" + s.Port
	msg := buildMessage(s.From, to, subject, body)
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	if err := smtp.SendMail(addr, auth, s.From, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// buildMessage assembles the minimal RFC 5322 envelope that SendMail needs.
// We keep it plain text — no MIME multipart, no HTML — which keeps the
// surface area for accidental injection small.
func buildMessage(from, to, subject, body string) string {
	// CRLF line endings are part of the spec; some servers (Postfix) are strict.
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	// Strip CR/LF from subject so a hostile caller can't inject extra headers.
	cleanSubject := strings.NewReplacer("\r", " ", "\n", " ").Replace(subject)
	fmt.Fprintf(&b, "Subject: %s\r\n", cleanSubject)
	fmt.Fprint(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprint(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprint(&b, "\r\n")
	b.WriteString(body)
	return b.String()
}
