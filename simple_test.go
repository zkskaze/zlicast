package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSimpleModeRotationAndInterrupt(t *testing.T) {
	app, err := newApp(t.TempDir(), "admin", "password", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer app.stop()
	handler := app.routes()
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"user":"admin","password":"password"}`)))
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	session := login.Result().Cookies()[0]
	call := func(method, path, mime string, body io.Reader, code int) *bytes.Buffer {
		t.Helper()
		r := httptest.NewRequest(method, path, body)
		r.AddCookie(session)
		r.Header.Set("Content-Type", mime)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("%s %s: %d, %s", method, path, w.Code, w.Body)
		}
		return w.Body
	}
	upload := func(category, station, frequency string) string {
		t.Helper()
		mp3, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency="+frequency+":duration=1.2", "-f", "mp3", "pipe:1").Output()
		if err != nil {
			t.Fatal(err)
		}
		var body bytes.Buffer
		m := multipart.NewWriter(&body)
		m.WriteField("category", category)
		m.WriteField("station", station)
		part, _ := m.CreateFormFile("file", category+".mp3")
		part.Write(mp3)
		m.Close()
		var out struct{ Media Media }
		err = json.NewDecoder(call("POST", "/api/media", m.FormDataContentType(), &body, 200)).Decode(&out)
		if err != nil || out.Media.ID == "" {
			t.Fatal("upload did not return a media ID", err)
		}
		return out.Media.ID
	}
	song := upload("music", "zlifm", "440")
	otherJingle := upload("jingles", "kaze", "550")
	jingle := upload("jingles", "zlifm", "660")
	news := upload("news", "zlifm", "880")
	const rule = "/api/stations/zlifm/jingle"
	call("PATCH", rule, "application/json", strings.NewReader(`{"every":2,"id":"`+otherJingle+`"}`), 400)
	call("PATCH", rule, "application/json", strings.NewReader(`{"every":2,"id":"`+jingle+`"}`), 200)

	app.startStation(app.state.Stations[0])
	server, client := memoryServer(handler)
	defer server.Close()
	stream, err := client.Get("http://radio.local/stream/zlifm")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	audioDone := make(chan int64, 1)
	go func() {
		n, _ := io.Copy(io.Discard, stream.Body)
		audioDone <- n
	}()
	waitFor := func(want []string) {
		t.Helper()
		until := time.Now().Add(7 * time.Second)
		for time.Now().Before(until) {
			app.mu.RLock()
			h := app.state.History["zlifm"]
			matches := len(h) >= len(want)
			if matches {
				for i, id := range want {
					matches = matches && h[i].ID == id
				}
			}
			app.mu.RUnlock()
			if matches {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("expected most recent broadcast %v", want)
	}
	waitFor([]string{song, jingle, song, song})
	call("POST", "/api/stations/zlifm/play", "application/json", strings.NewReader(`{"id":"`+news+`","interrupt":true}`), 200)
	waitFor([]string{song, news, song})
	select {
	case n := <-audioDone:
		t.Fatalf("listener disconnected after %d bytes", n)
	default:
	}
}
