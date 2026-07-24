package health

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/j27-aurum/gofast/internal/model"
)

// Caps for what we persist in ChannelHealth.last_failure_detail.
// Large enough for HTML error pages / playlist snippets; still bounded for SQLite.
const (
	maxErrorBody      = 32 * 1024
	maxFailureDetail  = 48 * 1024
	maxDetailURLRunes = 2048
)

func truncateDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= maxFailureDetail {
		return s
	}
	return s[:maxFailureDetail-1] + "…"
}

func failCheck(check model.HealthCheck, class, detail string) model.HealthCheck {
	check.Result = model.HealthCheckFailure
	check.FailureClass = class
	check.Detail = truncateDetail(detail)
	return check
}

func failFromErr(check model.HealthCheck, err error) model.HealthCheck {
	if err == nil {
		return failCheck(check, "unknown", "")
	}
	out := failCheck(check, failureClass(err), err.Error())
	var pe *probeHTTPError
	if errors.As(err, &pe) && pe.Status > 0 {
		out.HTTPStatus = pe.Status
	}
	return out
}

func failureClass(err error) string {
	if err == nil {
		return "unknown"
	}
	var pe *probeHTTPError
	if errors.As(err, &pe) && pe.Status > 0 {
		if pe.Status >= 500 && pe.Status <= 599 {
			return "http_5xx"
		}
		return fmt.Sprintf("http_%d", pe.Status)
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "403"):
		return "http_403"
	case strings.Contains(msg, "404"):
		return "http_404"
	case strings.Contains(msg, "401"):
		return "http_401"
	case strings.Contains(msg, "416"):
		return "http_416"
	case strings.Contains(msg, "429"):
		return "http_429"
	case strings.Contains(msg, "502"), strings.Contains(msg, "503"), strings.Contains(msg, "504"):
		return "http_5xx"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "no segments"):
		return "no_segments"
	case strings.Contains(msg, "tls") || strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "tls_error"
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "dns"):
		return "dns_error"
	case strings.Contains(msg, "connection refused"):
		return "conn_refused"
	case strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe"):
		return "conn_reset"
	case strings.Contains(msg, "eof"):
		return "eof"
	default:
		return "fetch_error"
	}
}

// formatHTTPFailure builds a multi-line probe detail for storage / UI.
func formatHTTPFailure(stage, rawURL, status, contentType string, body []byte, bodyTruncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s GET %s\n", stage, detailURL(rawURL))
	fmt.Fprintf(&b, "HTTP %s\n", status)
	if contentType != "" {
		fmt.Fprintf(&b, "Content-Type: %s\n", contentType)
	}
	fmt.Fprintf(&b, "Body-Length: %d", len(body))
	if bodyTruncated {
		b.WriteString("+ (truncated at read cap)")
	}
	b.WriteByte('\n')
	if len(body) == 0 {
		b.WriteString("\n(empty body)")
		return b.String()
	}
	b.WriteByte('\n')
	b.WriteString(bodyForDetail(body))
	return b.String()
}

func detailURL(raw string) string {
	if utf8.RuneCountInString(raw) <= maxDetailURLRunes {
		return raw
	}
	// Rare; keep head of URL for identity.
	r := []rune(raw)
	return string(r[:maxDetailURLRunes-1]) + "…"
}

func bodyForDetail(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if isMostlyText(body) {
		return string(body)
	}
	const hexMax = 4096
	n := len(body)
	if n > hexMax {
		n = hexMax
	}
	out := fmt.Sprintf("(binary, %d bytes) %x", len(body), body[:n])
	if len(body) > hexMax {
		out += "…"
	}
	return out
}

func isMostlyText(body []byte) bool {
	sample := body
	if len(sample) > 512 {
		sample = sample[:512]
	}
	if !utf8.Valid(sample) {
		return false
	}
	nonPrint := 0
	for _, b := range sample {
		if b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			nonPrint++
		}
	}
	return nonPrint*10 <= len(sample) // ≤10% control bytes
}
