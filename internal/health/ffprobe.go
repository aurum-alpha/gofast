package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// Proxy probe bases (optional): when Public is the emit origin and Internal is
// set, Check rewrites EmittedURL from Public → Internal for gen-side probes.

// FFProbe is Health L2: decode check via ffprobe (opt-in / on-demand).
type FFProbe struct {
	Path        string
	Timeout     time.Duration
	SoftRetries int
	// ProxyPublicBase / ProxyInternalBase rewrite EmittedURL for gen-side probes
	// (public M3U origin → Docker-internal origin). Empty Internal = no rewrite.
	ProxyPublicBase   string
	ProxyInternalBase string
}

// Check runs ffprobe -show_format -show_streams -of json against ProbeURL(ch).
// Requires at least one video stream. Passes RequestHeaders via -headers.
func (p *FFProbe) Check(ctx context.Context, ch model.Channel) model.HealthCheck {
	start := time.Now()
	at := start.UTC()
	check := model.HealthCheck{At: at, Source: "health_l2"}
	path := p.Path
	if path == "" {
		path = "/usr/bin/ffprobe"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	var pub, internal string
	if p != nil {
		pub, internal = p.ProxyPublicBase, p.ProxyInternalBase
	}
	streamURL := RewriteProxyProbeURL(ProbeURL(ch), pub, internal)
	if streamURL == "" {
		return finishCheck(failCheck(check, "no_url", "channel has no stream_url or emitted_url"), start)
	}
	check.FinalURL = streamURL

	retries := p.SoftRetries
	if retries < 0 {
		retries = 0
	}
	var last model.HealthCheck
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				out := failCheck(check, "timeout", "soft retry aborted: "+ctx.Err().Error())
				out.FinalURL = streamURL
				return finishCheck(out, start)
			case <-time.After(softRetryDelay):
			}
		}
		last = p.checkOnce(ctx, check, path, timeout, streamURL, ch.RequestHeaders)
		if last.Result == model.HealthCheckSuccess {
			return finishCheck(last, start)
		}
		if !isSoftFailure(last) || attempt == retries {
			if attempt > 0 && last.Detail != "" {
				last.Detail = fmt.Sprintf("retried after %s\n\n%s", softFailLabel(last), last.Detail)
			}
			return finishCheck(last, start)
		}
	}
	return finishCheck(last, start)
}

func (p *FFProbe) checkOnce(ctx context.Context, base model.HealthCheck, path string, timeout time.Duration, streamURL string, headers map[string]string) model.HealthCheck {
	check := base
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
	}
	if hdr := ffprobeHeaders(headers); hdr != "" {
		args = append(args, "-headers", hdr)
	}
	args = append(args, streamURL)

	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		} else {
			detail = fmt.Sprintf("%s (%v)", detail, err)
		}
		detail = fmt.Sprintf("ffprobe %s: %s", detailURL(streamURL), detail)
		if ctx.Err() != nil {
			return failCheck(check, "timeout", detail)
		}
		return failCheck(check, "ffprobe", detail)
	}

	var report struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format json.RawMessage `json:"format"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return failCheck(check, "ffprobe_parse",
			fmt.Sprintf("ffprobe json from %s: %v\n\n%s", detailURL(streamURL), err, bodyForDetail(stdout.Bytes())))
	}
	if len(report.Streams) == 0 || len(report.Format) == 0 || string(report.Format) == "null" {
		return failCheck(check, "ffprobe_empty",
			fmt.Sprintf("ffprobe %s returned no streams/format\n\n%s", detailURL(streamURL), bodyForDetail(stdout.Bytes())))
	}
	hasVideo := false
	for _, s := range report.Streams {
		if strings.EqualFold(s.CodecType, "video") {
			hasVideo = true
			break
		}
	}
	if !hasVideo {
		return failCheck(check, "ffprobe_no_video",
			fmt.Sprintf("ffprobe %s: no video stream\n\n%s", detailURL(streamURL), bodyForDetail(stdout.Bytes())))
	}
	check.Result = model.HealthCheckSuccess
	return check
}

func ffprobeHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	var b strings.Builder
	for k, v := range headers {
		if k == "" {
			continue
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return b.String()
}

// ProbeURL picks the URL for probes: EmittedURL when set (export/proxy path), else StreamURL.
func ProbeURL(ch model.Channel) string {
	if ch.EmittedURL != "" {
		return ch.EmittedURL
	}
	return ch.StreamURL
}

// RewriteProxyProbeURL replaces publicBase with internalBase when streamURL is
// under the public proxy origin. Used so gen can probe via Docker DNS while M3U
// still emits localhost/LAN for clients. Empty internalBase leaves streamURL unchanged.
func RewriteProxyProbeURL(streamURL, publicBase, internalBase string) string {
	if streamURL == "" || publicBase == "" || internalBase == "" {
		return streamURL
	}
	if streamURL == publicBase {
		return internalBase
	}
	prefix := publicBase + "/"
	if strings.HasPrefix(streamURL, prefix) {
		return internalBase + "/" + strings.TrimPrefix(streamURL, prefix)
	}
	return streamURL
}

// WithProbeURL returns a copy of ch with StreamURL/EmittedURL set to ProbeURL(ch).
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
