package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// FFProbe is L3: decode check via ffprobe (opt-in / on-demand).
type FFProbe struct {
	Path    string
	Timeout time.Duration
}

// Check runs ffprobe -show_format -show_streams -of json against the probe URL.
func (p *FFProbe) Check(ctx context.Context, ch model.Channel) model.HealthCheck {
	at := time.Now().UTC()
	check := model.HealthCheck{At: at, Source: "probe_l3"}
	path := p.Path
	if path == "" {
		path = "/usr/bin/ffprobe"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	streamURL := ch.EmittedURL
	if streamURL == "" {
		streamURL = ch.StreamURL
	}
	if streamURL == "" {
		check.Result = model.HealthCheckFailure
		check.FailureClass = "no_url"
		return check
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		streamURL,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		check.Result = model.HealthCheckFailure
		if ctx.Err() != nil {
			check.FailureClass = "timeout"
		} else {
			check.FailureClass = "ffprobe"
		}
		return check
	}

	var report struct {
		Streams []json.RawMessage `json:"streams"`
		Format  json.RawMessage   `json:"format"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		check.Result = model.HealthCheckFailure
		check.FailureClass = "ffprobe_parse"
		return check
	}
	if len(report.Streams) == 0 || len(report.Format) == 0 || string(report.Format) == "null" {
		check.Result = model.HealthCheckFailure
		check.FailureClass = "ffprobe_empty"
		return check
	}
	check.Result = model.HealthCheckSuccess
	return check
}

// ProbeURL picks the URL for L3: BEACON via EmittedURL (proxy) when set; else stream.
func ProbeURL(ch model.Channel) string {
	if ch.Classification == model.ClassBeacon && ch.EmittedURL != "" {
		return ch.EmittedURL
	}
	if ch.EmittedURL != "" {
		return ch.EmittedURL
	}
	return ch.StreamURL
}

// WithProbeURL returns a copy of ch with StreamURL/EmittedURL set for Check.
func WithProbeURL(ch model.Channel) model.Channel {
	u := ProbeURL(ch)
	out := ch
	out.EmittedURL = u
	out.StreamURL = u
	return out
}

// EnsureFFProbe reports whether path exists and is executable (best-effort).
func EnsureFFProbe(path string) error {
	if path == "" {
		path = "/usr/bin/ffprobe"
	}
	cmd := exec.Command(path, "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffprobe %q: %w", path, err)
	}
	return nil
}
