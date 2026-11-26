package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDisplayedRotationMatchesSkipAndShuffle(t *testing.T) {
	app, err := newApp(t.TempDir(), "admin", "password", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer app.stop()
	handler := app.routes()
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"user":"admin","password":"password"}`)))
	cookie := login.Result().Cookies()[0]
	call := func(method, target, body string, expected int) []byte {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != expected {
			t.Fatalf("%s %s: %d %s", method, target, recorder.Code, recorder.Body)
		}
		return recorder.Body.Bytes()
	}
	call("POST", "/api/stations", `{"slug":"test","name":"Test","description":"Evening radio","owner":"Radio Club","website":"https://radio.example"}`, 200)
	call("POST", "/api/stations", `{"slug":"test","name":"Test","website":"javascript:alert(1)"}`, 400)
	var station Station
	if err := json.Unmarshal(call("GET", "/api/stations/test/now-playing", "", 200), &station); err != nil ||
		station.Description != "Evening radio" || station.Owner != "Radio Club" || station.Website != "https://radio.example" {
		t.Fatalf("public station information: %+v (%v)", station, err)
	}

	source, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = app.addMedia(context.Background(), "admin", bytes.NewReader(source), "first.mp3", "music", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	e := app.engine("test")
	waitFor := func(match func(Status) bool) Status {
		t.Helper()
		for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			status := e.snapshot()
			if match(status) {
				return status
			}
		}
		t.Fatal("station did not reach the expected track")
		return Status{}
	}
	current := waitFor(func(status Status) bool { return status.Now.Category == "music" })
	waitFor(func(status Status) bool { return status.Elapsed >= 0.2 })
	var playing stationView
	if err := json.Unmarshal(call("GET", "/api/stations/test/now-playing", "", 200), &playing); err != nil ||
		playing.Status.Now.Duration < 6 || playing.Status.Now.Duration > 6.2 ||
		playing.Status.Elapsed <= current.Elapsed || playing.Status.Elapsed >= playing.Status.Now.Duration {
		t.Fatalf("public playback timing: %+v (%v)", playing.Status, err)
	}
	add := app.change("admin", "seed rotation", func(s *State) error {
		original := s.Media[0]
		for i := range 5 {
			item := original
			item.ID, item.Title = fmt.Sprintf("fixture-%d", i), fmt.Sprintf("Song %d", i)
			s.Media = append(s.Media, item)
		}
		return nil
	})
	if add != nil {
		t.Fatal(add)
	}
	preview := func() []Track {
		t.Helper()
		var dash struct{ Stations []stationView }
		if err := json.Unmarshal(call("GET", "/api/dashboard", "", 200), &dash); err != nil {
			t.Fatal(err)
		}
		for _, s := range dash.Stations {
			if s.Slug == "test" {
				return s.Upcoming
			}
		}
		t.Fatal("test station missing")
		return nil
	}
	before := preview()
	call("POST", "/api/stations/test/shuffle", "", 200)
	after := preview()
	if len(before) != 5 || len(after) != 5 || before[0].ID == after[0].ID {
		t.Fatalf("shuffle did not update the displayed queue: %v -> %v", before, after)
	}
	if !e.snapshot().Started.Equal(current.Started) {
		t.Fatal("shuffle interrupted current track")
	}
	body, _ := json.Marshal(map[string]time.Time{"started": current.Started})
	call("POST", "/api/stations/test/skip", string(body), 200)
	skipped := waitFor(func(status Status) bool { return status.Now.ID == after[0].ID })
	if !skipped.Started.After(current.Started) || skipped.Elapsed >= playing.Status.Elapsed {
		t.Fatalf("skip did not reset playback timing: %+v", skipped)
	}
	call("POST", "/api/stations/test/skip", string(body), 409)
	if preview()[0].ID != after[1].ID {
		t.Fatal("skipping did not consume the displayed next track")
	}
}
