package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOpsTimezone = "America/Los_Angeles"
	defaultOpsSendAt   = "00:00"
	defaultSMTPPort    = 587
)

// OpsReport is the daily email ops digest (schedule + addressing + SMTP server).
type OpsReport struct {
	Enabled  *bool         `yaml:"enabled" json:"enabled"`
	Timezone string        `yaml:"timezone" json:"timezone"` // IANA; default America/Los_Angeles
	SendAt   string        `yaml:"send_at" json:"send_at"`   // "HH:MM"; default "00:00"
	SMTP     OpsReportSMTP `yaml:"smtp" json:"smtp"`
	From     string        `yaml:"from" json:"from"`
	To       []string      `yaml:"to" json:"to"`
}

// OpsReportSMTP is the mail *server* only (not From/To addressing).
type OpsReportSMTP struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`         // default 587
	STARTTLS *bool  `yaml:"starttls" json:"starttls"` // default true
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"` // YAML ok; FASTGEN_SMTP_PASSWORD wins
}

// IsEnabled reports whether the daily ops report is on.
func (o OpsReport) IsEnabled() bool {
	return o.Enabled != nil && *o.Enabled
}

// Location loads the configured IANA timezone (default America/Los_Angeles).
func (o OpsReport) Location() (*time.Location, error) {
	name := strings.TrimSpace(o.Timezone)
	if name == "" {
		name = defaultOpsTimezone
	}
	return time.LoadLocation(name)
}

// TimezoneOrDefault returns the configured IANA name or the product default.
func (o OpsReport) TimezoneOrDefault() string {
	name := strings.TrimSpace(o.Timezone)
	if name == "" {
		return defaultOpsTimezone
	}
	return name
}

// SendAtOrDefault returns HH:MM or the product default.
func (o OpsReport) SendAtOrDefault() string {
	s := strings.TrimSpace(o.SendAt)
	if s == "" {
		return defaultOpsSendAt
	}
	return s
}

// ParseSendAt parses HH:MM into hour and minute (24h).
func ParseSendAt(s string) (hour, minute int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = defaultOpsSendAt
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("send_at must be HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("send_at hour must be 0–23")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("send_at minute must be 0–59")
	}
	return h, m, nil
}

// PortOrDefault returns the SMTP port (default 587).
func (s OpsReportSMTP) PortOrDefault() int {
	if s.Port <= 0 {
		return defaultSMTPPort
	}
	return s.Port
}

// STARTTLSOrDefault returns whether STARTTLS is enabled (default true).
func (s OpsReportSMTP) STARTTLSOrDefault() bool {
	if s.STARTTLS == nil {
		return true
	}
	return *s.STARTTLS
}

// PasswordSet reports whether an effective SMTP password is configured.
func (o OpsReport) PasswordSet() bool {
	return o.SMTP.Password != ""
}

// validateOpsReport checks schedule/SMTP when the report is enabled.
func (c *Config) validateOpsReport() error {
	if c == nil || !c.OpsReport.IsEnabled() {
		return nil
	}
	o := c.OpsReport
	if strings.TrimSpace(o.SMTP.Host) == "" {
		return fmt.Errorf("config: ops_report.smtp.host is required when enabled")
	}
	if o.SMTP.Port < 0 {
		return fmt.Errorf("config: ops_report.smtp.port must be >= 0")
	}
	if strings.TrimSpace(o.From) == "" {
		return fmt.Errorf("config: ops_report.from is required when enabled")
	}
	to := normalizeEmailList(o.To)
	if len(to) == 0 {
		return fmt.Errorf("config: ops_report.to requires at least one recipient when enabled")
	}
	if _, err := o.Location(); err != nil {
		return fmt.Errorf("config: ops_report.timezone: %w", err)
	}
	if _, _, err := ParseSendAt(o.SendAtOrDefault()); err != nil {
		return fmt.Errorf("config: ops_report.send_at: %w", err)
	}
	return nil
}

func normalizeEmailList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
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

func mergeOpsReport(dst, src *OpsReport) {
	if dst == nil || src == nil {
		return
	}
	if src.Enabled != nil {
		v := *src.Enabled
		dst.Enabled = &v
	}
	if src.Timezone != "" {
		dst.Timezone = src.Timezone
	}
	if src.SendAt != "" {
		dst.SendAt = src.SendAt
	}
	if src.From != "" {
		dst.From = src.From
	}
	if src.To != nil {
		dst.To = append([]string(nil), src.To...)
	}
	if src.SMTP.Host != "" {
		dst.SMTP.Host = src.SMTP.Host
	}
	if src.SMTP.Port != 0 {
		dst.SMTP.Port = src.SMTP.Port
	}
	if src.SMTP.STARTTLS != nil {
		v := *src.SMTP.STARTTLS
		dst.SMTP.STARTTLS = &v
	}
	if src.SMTP.Username != "" {
		dst.SMTP.Username = src.SMTP.Username
	}
	if src.SMTP.Password != "" {
		dst.SMTP.Password = src.SMTP.Password
	}
}
