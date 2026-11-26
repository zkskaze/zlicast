package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
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
	before := call("GET", "/api/dashboard", "", nil, 200)
	if bytes.Contains(before.Bytes(), []byte("one-off")) {
		t.Fatal("test bulletin is already in the library")
	}
	mp3, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=970:duration=1.2", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "one-off.mp3")
	part.Write(mp3)
	form.Close()
	call("POST", "/api/stations/zlifm/bulletin", form.FormDataContentType(), &body, 200)
	var temporary Track
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		now := app.engine("zlifm").snapshot().Now
		if now.Title == "one-off" {
			temporary = now
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !temporary.Temporary {
		t.Fatal("bulletin did not play as a temporary track")
	}
	until = time.Now().Add(5 * time.Second)
	for app.engine("zlifm").snapshot().Now.ID == temporary.ID && time.Now().Before(until) {
		time.Sleep(20 * time.Millisecond)
	}
	if app.engine("zlifm").snapshot().Now.ID == temporary.ID {
		t.Fatal("regular rotation did not resume")
	}
	if _, err := os.Stat(temporary.Path); !os.IsNotExist(err) {
		t.Fatal("bulletin file was retained after playing", err)
	}
	app.mu.RLock()
	_, saved := findMedia(&app.state, temporary.ID)
	app.mu.RUnlock()
	if saved {
		t.Fatal("one-off bulletin was saved to the library")
	}
	call("POST", "/api/stations/zlifm/shuffle", "", nil, 200)
	deleted := call("POST", "/api/media/bulk-delete", "application/json", strings.NewReader(`{"ids":["`+news+`","`+otherJingle+`"]}`), 200)
	if strings.TrimSpace(deleted.String()) != `{"deleted":2}` {
		t.Fatal(deleted.String())
	}
	select {
	case n := <-audioDone:
		t.Fatalf("listener disconnected after %d bytes", n)
	default:
	}
}

func TestShuffleCompletesRoundWithFiveHundredSongs(t *testing.T) {
	now := time.Now()
	var rot rotation
	songs := make([]Media, 500)
	for i := range songs {
		songs[i].Track = Track{ID: strconv.Itoa(i), Category: "music"}
	}
	seen := map[string]bool{}
	var last Track
	for i := range songs {
		if i == 250 {
			rot.rank = nil // The admin action reshuffles the rest of the round.
		}
		choice := rot.pick("music", songs, nil, Rules{}, now)
		if seen[choice.ID] {
			t.Fatalf("song %s repeated at position %d", choice.ID, i)
		}
		seen[choice.ID] = true
		last = choice.Track
	}
	if choice := rot.pick("music", songs, []Entry{{At: now, Track: last}}, Rules{}, now); choice.ID == last.ID {
		t.Fatal("last song repeated across rounds")
	}
}
