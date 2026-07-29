package opsreport

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
)

// Mailer sends multipart/alternative messages over SMTP.
type Mailer struct {
	// DialTimeout bounds TCP connect (tests may override).
	DialTimeout time.Duration
}

// Send delivers subject/text/html from cfg.OpsReport addressing via SMTP.
func (m *Mailer) Send(cfg config.OpsReport, subject, text, htmlBody string) error {
	host := strings.TrimSpace(cfg.SMTP.Host)
	if host == "" {
		return fmt.Errorf("opsreport: smtp host is empty")
	}
	port := cfg.SMTP.PortOrDefault()
	from := strings.TrimSpace(cfg.From)
	to := normalizeTo(cfg.To)
	if from == "" {
		return fmt.Errorf("opsreport: from is empty")
	}
	if len(to) == 0 {
		return fmt.Errorf("opsreport: no recipients")
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	msg := buildMIME(from, to, subject, text, htmlBody)

	timeout := m.DialTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("opsreport: smtp dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("opsreport: smtp client: %w", err)
	}
	defer client.Close()

	if cfg.SMTP.STARTTLSOrDefault() {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
			if err := client.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("opsreport: starttls: %w", err)
			}
		}
	}

	user := strings.TrimSpace(cfg.SMTP.Username)
	pass := cfg.SMTP.Password
	if user != "" || pass != "" {
		auth := smtp.PlainAuth("", user, pass, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("opsreport: smtp auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("opsreport: mail from: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("opsreport: rcpt %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("opsreport: data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("opsreport: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("opsreport: close data: %w", err)
	}
	return client.Quit()
}

func normalizeTo(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		addr := strings.TrimSpace(raw)
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}

func buildMIME(from string, to []string, subject, text, htmlBody string) []byte {
	boundary := "gofast-ops-boundary"
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + encodeSubject(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n")
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(normalizeNewlines(text))
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(normalizeNewlines(htmlBody))
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

func encodeSubject(s string) string {
	if isASCII(s) {
		return s
	}
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}
