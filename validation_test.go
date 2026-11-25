package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRolesAndEmbeddedUI(t *testing.T) {
	a, err := newApp(t.TempDir(), "admin", "local-pass", "key", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer a.stop()
	a.startStation(a.state.Stations[0])
	h := a.routes()
	call := func(method, path, body string, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	root := call("POST", "/api/login", `{"user":"admin","password":"local-pass"}`, "", nil)
	if root.Code != 200 {
		t.Fatal(root.Body.String())
	}
	cookie := root.Result().Cookies()[0]
	tok := call("POST", "/api/admin/tokens", `{"name":"reader","role":"viewer"}`, "", cookie)
	var v struct{ Token string }
	json.Unmarshal(tok.Body.Bytes(), &v)
	if v.Token == "" {
		t.Fatal(tok.Body.String())
	}
	for _, p := range []string{"/api/dashboard", "/api/system", "/api/stations/zlifm/stats"} {
		if w := call("GET", p, "", v.Token, nil); w.Code != 200 {
			t.Fatal(p, w.Code)
		}
	}
	for _, p := range []string{"/api/stations", "/api/admin/users", "/api/media"} {
		if w := call("POST", p, "{}", v.Token, nil); w.Code != 403 {
			t.Fatal(p, w.Code)
		}
	}
	for _, p := range []string{"/", "/listen/zlifm"} {
		w := call("GET", p, "", "", nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "modulepreload") {
			t.Fatal("SPA missing at " + p)
		}
		for _, match := range regexp.MustCompile(`href="(/_app/[^"]+)"`).FindAllStringSubmatch(w.Body.String(), -1) {
			if w := call("GET", match[1], "", "", nil); w.Code != 200 || w.Body.Len() < 10 {
				t.Fatal("asset missing " + match[1])
			}
		}
	}
	if w := call("HEAD", "/stream/zlifm", "", "", nil); w.Code != 200 || w.Header().Get("Content-Type") != "audio/mpeg" || w.Body.Len() != 0 {
		t.Fatal("HEAD failed")
	}
}
func TestSilentTrackFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "silence.wav")
	n := 192000 * 8
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(n+36))
	b.WriteString("WAVEfmt ")
	for _, x := range []any{uint32(16), uint16(1), uint16(2), uint32(48000), uint32(192000), uint16(4), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, x)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(n))
	b.Write(make([]byte, n))
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	e := newEngine(ProgramConfig{Name: "Test", Format: "mp3", Bitrate: 128}, func() Track { return Track{ID: "silent", Title: "Silent", Path: path, Category: "music"} }, func(Track) {})
	defer e.close()
	server, c := memoryServer(http.HandlerFunc(e.stream))
	defer server.Close()
	res, err := c.Get("http://radio.local/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	end := time.Now().Add(9 * time.Second)
	buf := make([]byte, 4096)
	for time.Now().Before(end) {
		if _, err = io.ReadFull(res.Body, buf); err != nil {
			t.Fatal(err)
		}
		s := e.snapshot()
		if s.Now.ID == "@theme" && s.Errors > 0 && s.Level > .01 {
			return
		}
	}
	t.Fatal("silent source never switched to audible fallback")
}
func TestCalendarAndJingleWindows(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Belgrade")
	date := time.Date(2026, 11, 25, 9, 0, 0, 0, loc)
	for _, tc := range []struct {
		repeat, date string
		want         bool
	}{{"daily", "2026-10-05", true}, {"weekly", "2026-11-18", true}, {"weekly", "2026-11-19", false}, {"monthly", "2026-10-25", true}, {"once", "2026-11-26", false}} {
		if matchEvent(Event{Time: "09:00", Repeat: tc.repeat, Date: tc.date}, date) != tc.want {
			t.Fatal(tc)
		}
	}
	if !jingleTime([]string{"јутро", "датум:11-25"}, date) || jingleTime([]string{"вече"}, date) || jingleTime([]string{"датум:12-31"}, date) {
		t.Fatal("jingle selection")
	}
}
