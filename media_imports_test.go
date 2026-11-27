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

func TestMediaImportContinuesAfterUploadRequest(t *testing.T) {
	app, err := newApp(t.TempDir(), "admin", "password", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer app.stop()
	handler := app.routes()
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"user":"admin","password":"password"}`)))
	session := login.Result().Cookies()[0]
	call := func(method, target, mime string, body io.Reader, expected int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, target, body)
		r.AddCookie(session)
		r.Header.Set("Content-Type", mime)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s %s: %d %s", method, target, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	webm, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=880:duration=2", "-c:a", "libopus", "-f", "webm", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	post := func(input []byte) string {
		t.Helper()
		var payload bytes.Buffer
		form := multipart.NewWriter(&payload)
		form.WriteField("category", "music")
		part, _ := form.CreateFormFile("file", "radio.webm")
		part.Write(input)
		form.Close()
		var submitted struct{ Job string }
		if err := json.Unmarshal(call("POST", "/api/media?async=1", form.FormDataContentType(), &payload, 202), &submitted); err != nil || submitted.Job == "" {
			t.Fatal("missing job", err)
		}
		return submitted.Job
	}
	waitFor := func(id, expected string) mediaImport {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			var status mediaImport
			if err := json.Unmarshal(call("GET", "/api/media/imports/"+id, "", nil, 200), &status); err != nil {
				t.Fatal(err)
			}
			if status.Status != "processing" {
				if status.Status != expected {
					t.Fatal(status.Status, status.Error)
				}
				return status
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("media import timed out")
		return mediaImport{}
	}
	status := waitFor(post(webm), "complete")
	if status.Media == nil || status.Media.Duration < 1.9 || status.Duplicate {
		t.Fatalf("unexpected import result: %+v", status)
	}
	if retry := waitFor(post(webm), "complete"); !retry.Duplicate || retry.Media.ID != status.Media.ID {
		t.Fatal("retry should reuse the first import")
	}
	if failed := waitFor(post([]byte("not audio")), "failed"); failed.Error == "" {
		t.Fatal("failed import did not expose an error")
	}

	target := "/api/media/" + status.Media.ID
	call("PATCH", target, "application/json", strings.NewReader(`{"title":" "}`), 400)
	call("PATCH", target, "application/json", strings.NewReader(`{"title":" Нови наслов "}`), 200)
	renamed := waitFor(post(webm), "complete")
	if !renamed.Duplicate || renamed.Media == nil || renamed.Media.ID != status.Media.ID ||
		renamed.Media.Hash != status.Media.Hash || renamed.Media.Title != "Нови наслов" {
		t.Fatalf("renaming should preserve the original media and its audio: %+v", renamed)
	}
}
