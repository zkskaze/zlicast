package main

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/net/websocket"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHLSOutput(t *testing.T) {
	e := newEngine(ProgramConfig{Name: "Kaze", Format: "aac", Bitrate: 128, HLS: true}, func() Track { return Track{} }, func(Track) {})
	defer e.close()
	var playlist string
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		e.mu.Lock()
		dir := e.hlsDir
		e.mu.Unlock()
		if dir != "" {
			path := filepath.Join(dir, "index.m3u8")
			if b, err := os.ReadFile(path); err == nil && bytes.Contains(b, []byte("#EXTINF")) {
				playlist = path
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	if playlist == "" {
		t.Fatalf("HLS playlist missing: %+v", e.snapshot())
	}
	b, _ := os.ReadFile(playlist)
	var ts string
	for _, s := range strings.Split(string(b), "\n") {
		if hlsSegment.MatchString(strings.TrimSpace(s)) {
			ts = filepath.Join(filepath.Dir(playlist), strings.TrimSpace(s))
			break
		}
	}
	if ts == "" {
		t.Fatal(string(b))
	}
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", ts, "-f", "s16le", "-t", "1", "pipe:1").Output()
	if err != nil || len(out) < 96000 {
		t.Fatal(err, len(out))
	}
}
func TestLiveStudioHandover(t *testing.T) {
	a, err := newApp(t.TempDir(), "admin", "studio-pass", "source-key", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	a.startStation(a.state.Stations[0])
	defer a.stop()
	server, c := memoryServer(a.routes())
	defer server.Close()
	c.Jar, _ = cookiejar.New(nil)
	res, err := c.Post("http://radio.local/api/login", "application/json", strings.NewReader(`{"user":"admin","password":"studio-pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	audience, err := c.Get("http://radio.local/stream/zlifm")
	if err != nil {
		t.Fatal(err)
	}
	defer audience.Body.Close()
	if _, err = io.ReadFull(audience.Body, make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	addr, _ := url.Parse("http://radio.local")
	cfg, _ := websocket.NewConfig("ws://radio.local/api/stations/zlifm/live?title=OnAir", addr.String())
	for _, cookie := range c.Jar.Cookies(addr) {
		cfg.Header.Add("Cookie", cookie.String())
	}
	conn, err := c.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "radio.local")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := websocket.NewClient(cfg, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for i := 0; i < 20; i++ {
		if err := websocket.Message.Send(ws, themeAudio()[i*blockBytes:(i+1)*blockBytes]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !a.engine("zlifm").snapshot().Live {
		t.Fatal("live audio never reached the encoder")
	}
	if _, err = io.ReadFull(audience.Body, make([]byte, 4000)); err != nil {
		t.Fatal(err)
	}
	ws.Close()
	until := time.Now().Add(2 * time.Second)
	for a.engine("zlifm").snapshot().Live && time.Now().Before(until) {
		time.Sleep(20 * time.Millisecond)
	}
	s := a.engine("zlifm").snapshot()
	if s.Live || s.Now.Category != "fallback" {
		t.Fatal(fmt.Sprint(s))
	}
	if _, err = io.ReadFull(audience.Body, make([]byte, 4000)); err != nil {
		t.Fatal(err)
	}
}
