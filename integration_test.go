package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalRadioWorkflow(t *testing.T) {
	dir := t.TempDir()
	loc, _ := time.LoadLocation("Europe/Belgrade")
	a, err := newApp(dir, "admin", "test-password", "source-secret", loc)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range a.state.Stations {
		a.startStation(s)
	}
	s, c := memoryServer(a.routes())
	defer s.Close()
	jar, _ := cookiejar.New(nil)
	c.Jar = jar
	call := func(method, path, ct string, body io.Reader, code int) []byte {
		t.Helper()
		r, _ := http.NewRequest(method, "http://radio.local"+path, body)
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		res, e := c.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != code {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, b)
		}
		return b
	}
	call("GET", "/api/dashboard", "", nil, 401)
	call("POST", "/api/login", "application/json", strings.NewReader(`{"user":"admin","password":"test-password"}`), 200)
	data, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=659:duration=3", "-metadata", "title=Test signal", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		mp := multipart.NewWriter(&buf)
		mp.WriteField("category", "music")
		f, _ := mp.CreateFormFile("file", "unit.mp3")
		f.Write(data)
		mp.Close()
		b := call("POST", "/api/media", mp.FormDataContentType(), &buf, 200)
		var v struct {
			Media     Media
			Duplicate bool
		}
		json.Unmarshal(b, &v)
		if v.Duplicate != (i == 1) || v.Media.Title != "Test signal" || v.Media.Duration < 2.9 {
			t.Fatalf("%s", b)
		}
		id = v.Media.ID
	}
	stream, err := c.Get("http://radio.local/stream/zlifm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(stream.Body, make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/stations/zlifm/play", "application/json", strings.NewReader(fmt.Sprintf(`{"id":"%s","interrupt":true}`, id)), 200)
	deadline := time.Now().Add(5 * time.Second)
	for a.engine("zlifm").snapshot().Now.ID != id && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if a.engine("zlifm").snapshot().Now.ID != id {
		t.Fatal("import did not play")
	}
	if _, err = io.ReadFull(stream.Body, make([]byte, 12000)); err != nil {
		t.Fatal("lost listener", err)
	}
	stream.Body.Close()
	call("POST", "/api/stations", "application/json", strings.NewReader(`{"slug":"lab","name":"Lab","bitrate":96,"format":"aac"}`), 200)
	call("POST", "/api/playlists", "application/json", strings.NewReader(fmt.Sprintf(`{"name":"Rotation","station":"lab","tracks":["%s"]}`, id)), 200)
	when := time.Now().In(loc)
	event := fmt.Sprintf(`{"name":"Bulletin","station":"lab","date":"%s","time":"%s","repeat":"daily","target":"track","value":"%s","interrupt":true}`, when.Format("2006-01-02"), when.Format("15:04"), id)
	call("POST", "/api/events", "application/json", strings.NewReader(event), 200)
	a.tick(when)
	a.mu.RLock()
	last := a.state.Events[0].Last
	a.mu.RUnlock()
	if last == "" {
		t.Fatal("event not recorded")
	}
	archive := call("GET", "/api/admin/backup", "", nil, 200)
	call("POST", "/api/admin/restore", "application/zip", bytes.NewReader(archive), 200)
	call("DELETE", "/api/stations/lab", "", nil, 200)
	a.stop()
	b, err := newApp(dir, "admin", "test-password", "source-secret", loc)
	if err != nil {
		t.Fatal(err)
	}
	defer b.stop()
	if err = b.restorePending(); err != nil {
		t.Fatal(err)
	}
	if len(b.state.Media) != 1 || len(b.state.Stations) != 3 || len(b.state.Events) != 1 {
		t.Fatal("backup or persistence mismatch")
	}
	if _, err := os.Stat(filepath.Join(dir, "zlicast.sqlite")); err != nil {
		t.Fatal(err)
	}
}
