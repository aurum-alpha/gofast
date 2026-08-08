package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestRedactFFmpegArgv(t *testing.T) {
	got := redactFFmpegArgv([]string{"-i", "http://x", "-headers", "Cookie: secret\r\n", "-f", "mpegts"})
	if strings.Contains(got, "secret") {
		t.Fatalf("headers not redacted: %q", got)
	}
	if !strings.Contains(got, "-headers [redacted]") {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateDemuxLog(t *testing.T) {
	if truncateDemuxLog("abc", 10) != "abc" {
		t.Fatal("short")
	}
	got := truncateDemuxLog(strings.Repeat("x", 20), 8)
	if got != "xxxxxxxx…" {
		t.Fatalf("got %q", got)
	}
}

func TestDemuxCloseReason(t *testing.T) {
	if demuxCloseReason(true, errors.New("copy"), errors.New("wait")) != ReasonClientCancel {
		t.Fatal("client cancel")
	}
	if demuxCloseReason(false, errors.New("copy"), nil) != ReasonFFmpegExit {
		t.Fatal("copy err")
	}
	if demuxCloseReason(false, nil, errors.New("exit status 1")) != ReasonFFmpegExit {
		t.Fatal("wait err without ExitError still ffmpeg_exit")
	}
	if demuxCloseReason(false, nil, nil) != "" {
		t.Fatal("clean end")
	}

	// Kill reap after clean EOF: ExitError with SIGKILL → empty reason.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Process.Kill()
	waitErr := cmd.Wait()
	if demuxCloseReason(false, nil, waitErr) != "" {
		t.Fatalf("kill after EOF: reason=%q wait=%v", demuxCloseReason(false, nil, waitErr), waitErr)
	}
	if demuxCloseReason(false, errors.New("copy"), waitErr) != ReasonFFmpegExit {
		t.Fatal("kill with copy err")
	}
	_, sig := ffmpegWaitInfo(waitErr)
	if !isKillSignal(sig) {
		t.Fatalf("expected kill signal, got %q from %v", sig, waitErr)
	}
}

func TestFFmpegWaitInfoNil(t *testing.T) {
	code, sig := ffmpegWaitInfo(nil)
	if code != 0 || sig != "" {
		t.Fatalf("code=%d sig=%q", code, sig)
	}
	code, sig = ffmpegWaitInfo(errors.New("plain"))
	if code != -1 || sig != "" {
		t.Fatalf("plain code=%d sig=%q", code, sig)
	}
}

func TestIsKillSignal(t *testing.T) {
	if !isKillSignal("killed") || !isKillSignal("SIGKILL") {
		t.Fatal("expected kill")
	}
	if isKillSignal("terminated") {
		t.Fatal("term")
	}
}

func TestServeDemuxStableSlotsFull(t *testing.T) {
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "0")
	origin := NewStaticOrigin()
	origin.Set(model.ProviderPluto, "ch1", ChannelOrigin{
		StreamURL: "https://cdn.example/live.m3u8", Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/ch1.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServeDemuxStableNativePipe(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf 'Gfake-mpegts-bytes-here!!!!'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FASTPROXY_FFMPEG", stub)
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "2")

	origin := NewStaticOrigin()
	origin.Set(model.ProviderPluto, "news", ChannelOrigin{
		StreamURL: "https://cdn.example/live.m3u8", Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/news.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("Content-Type=%q", ct)
	}
	if !strings.Contains(rec.Body.String(), "fake-mpegts") {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestResolveDemuxIngestAmagiLoopback(t *testing.T) {
	h := NewHandler(NewStaticOrigin(), NewStore(), nil)
	h.LoopbackBase = "http://127.0.0.1:8181"
	u, hdr, err := h.resolveDemuxIngest(context.Background(), model.ProviderLG, "x", ChannelOrigin{
		StreamURL:      "https://amagi.example/beacon.m3u8",
		Classification: model.ClassAmagiSSAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u != "http://127.0.0.1:8181/stream/lg/x.m3u8" {
		t.Fatalf("url=%q", u)
	}
	if hdr != nil {
		t.Fatalf("loopback should not forward upstream headers to ffmpeg, got %v", hdr)
	}
}

func TestDemuxStableSnapshotRows(t *testing.T) {
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "4")
	tr := newDemuxStableTracker()
	id, slot, ok := tr.acquire(model.ProviderPluto, "a")
	if !ok {
		t.Fatal("acquire")
	}
	slot.bytesOut.Store(1000)
	slot.state.Store("streaming")
	active, max, _, sessions := tr.snapshotRows()
	if active != 1 || max != 4 || len(sessions) != 1 {
		t.Fatalf("active=%d max=%d sessions=%+v", active, max, sessions)
	}
	if sessions[0].ChannelID != "a" || sessions[0].BytesOut != 1000 {
		t.Fatalf("%+v", sessions[0])
	}
	tr.release(id)
	active, _, _, sessions = tr.snapshotRows()
	if active != 0 || len(sessions) != 0 {
		t.Fatalf("after release active=%d sessions=%+v", active, sessions)
	}
}

func TestServeDemuxStableOriginMiss(t *testing.T) {
	h := NewHandler(NewStaticOrigin(), NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/missing.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestDemuxFFmpegArgsMidRollHarden(t *testing.T) {
	h := NewHandler(NewStaticOrigin(), NewStore(), nil)
	h.demux = newDemuxStableTracker()
	args := h.demuxFFmpegArgs("https://cdn.example/live.m3u8", nil)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-reinit_filter 1",
		"-reconnect_at_eof 1",
		"-rw_timeout",
		"-map 0:v:0",
		"-map 0:a:0",
		"-fps_mode cfr",
		"-crf 23",
		"eval=frame",
		"-max_muxing_queue_size",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "0:v:0?") || strings.Contains(joined, "0:a:0?") {
		t.Fatalf("optional maps must not be used: %q", joined)
	}
}

func TestDemuxShouldRestart(t *testing.T) {
	if demuxShouldRestart(nil, nil) {
		t.Fatal("clean EOF must not restart")
	}
	if !demuxShouldRestart(nil, errors.New("exit status 1")) {
		t.Fatal("ffmpeg exit should restart")
	}
	if demuxShouldRestart(errors.New("broken pipe"), errors.New("exit status 1")) {
		t.Fatal("copy err must not restart")
	}
}

func TestServeDemuxStableRestartsAfterFFmpegExit(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "ffmpeg")
	count := filepath.Join(dir, "count")
	script := "#!/bin/sh\n" +
		"c=\"" + count + "\"\n" +
		"n=0\n" +
		"if [ -f \"$c\" ]; then n=$(cat \"$c\"); fi\n" +
		"n=$((n+1))\n" +
		"echo \"$n\" > \"$c\"\n" +
		"if [ \"$n\" = \"1\" ]; then\n" +
		"  printf 'chunk-one'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'chunk-two'\n" +
		"exit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FASTPROXY_FFMPEG", stub)
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "2")

	origin := NewStaticOrigin()
	origin.Set(model.ProviderPluto, "news", ChannelOrigin{
		StreamURL: "https://cdn.example/live.m3u8", Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/news.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "chunk-one") || !strings.Contains(body, "chunk-two") {
		t.Fatalf("expected both encode attempts, body=%q", body)
	}
}
