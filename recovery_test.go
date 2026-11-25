package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

func TestEncoderRecoveryKeepsConnection(t *testing.T) {
	e := newEngine(ProgramConfig{Name: "Kaze", Format: "mp3", Bitrate: 128}, func() Track { return Track{} }, func(Track) {})
	defer e.close()
	srv, c := memoryServer(http.HandlerFunc(e.stream))
	defer srv.Close()
	r, err := c.Get("http://radio.local/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	buf := make([]byte, 8192)
	if _, err = io.ReadFull(r.Body, buf); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	kill := e.encoderCancel
	e.mu.Unlock()
	if kill == nil {
		t.Fatal("encoder missing")
	}
	kill()
	end := time.Now().Add(7 * time.Second)
	for time.Now().Before(end) {
		if _, err = io.ReadFull(r.Body, buf); err != nil {
			t.Fatal(err)
		}
		s := e.snapshot()
		if s.OnAir && s.Errors > 0 && s.Listeners == 1 {
			return
		}
	}
	t.Fatal("encoder failed to recover")
}
func TestExternalIcecastSource(t *testing.T) {
	a, err := newApp(t.TempDir(), "admin", "local", "source-secret", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer a.stop()
	a.startStation(a.state.Stations[0])
	mp3, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=523:duration=3", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/source/zlifm", bytes.NewReader(mp3))
	r.SetBasicAuth("source", "source-secret")
	r.Header.Set("Content-Type", "audio/mpeg")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { a.routes().ServeHTTP(w, r); close(done) }()
	observed := false
	timer := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-done:
			if !observed || w.Code != 200 {
				t.Fatal("source was never audible", w.Code)
			}
			return
		case <-timer.C:
			s := a.engine("zlifm").snapshot()
			observed = observed || s.Live && s.Level > .01
		case <-deadline:
			t.Fatal("source timed out")
		}
	}
}
