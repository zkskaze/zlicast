package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestYouTubeVideoID(t *testing.T) {
	const id = "YE7VzlLtp-4"
	tests := []struct {
		link string
		want string
	}{
		{" https://www.youtube.com/watch?v=" + id + "&list=PL123&t=24s ", id},
		{"https://youtu.be/" + id + "?si=share", id},
		{"https://music.youtube.com/watch?v=" + id, id},
		{"https://www.youtube.com/shorts/" + id, id},
		{"https://m.youtube.com/live/" + id, id},
		{"https://www.youtube.com/embed/" + id, id},
		{"https://www.youtube.com/playlist?list=PL123", ""},
		{"https://www.youtube.com/redirect?v=" + id, ""},
		{"https://www.youtube.com.example.org/watch?v=" + id, ""},
		{"https://example.org@www.youtube.com/watch?v=" + id, ""},
		{"https://www.youtube.com:444/watch?v=" + id, ""},
		{"https://youtu.be/" + id + "/extra", ""},
		{"https://youtu.be/%2F" + id, ""},
		{"https://www.youtube.com/watch?v=notvalid", ""},
		{"file://youtu.be/" + id, ""},
		{"--exec", ""},
	}
	for _, test := range tests {
		if got := youtubeVideoID(test.link); got != test.want {
			t.Errorf("youtubeVideoID(%q) = %q, want %q", test.link, got, test.want)
		}
	}
}

func TestYouTubeImportValidatesBeforeDownload(t *testing.T) {
	app, err := newApp(t.TempDir(), "admin", "password", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer app.stop()
	handler := app.routes()
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"user":"admin","password":"password"}`)))
	cookies := login.Result().Cookies()
	if login.Code != http.StatusOK || len(cookies) == 0 {
		t.Fatal("login did not return a session", login.Code)
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("ZLICAST_YT_DLP", "")
	tests := []struct {
		body       string
		authorized bool
		status     int
	}{
		{`{"url":"https://youtu.be/YE7VzlLtp-4"}`, false, http.StatusUnauthorized},
		{`{"url":"https://example.org/watch?v=YE7VzlLtp-4"}`, true, http.StatusBadRequest},
		{`{"url":"https://youtu.be/YE7VzlLtp-4","station":"missing"}`, true, http.StatusBadRequest},
		{`{"url":"https://youtu.be/YE7VzlLtp-4","category":"missing"}`, true, http.StatusBadRequest},
		{`{"url":"https://youtu.be/YE7VzlLtp-4","expires":"soon"}`, true, http.StatusBadRequest},
		{`{"url":"https://youtu.be/YE7VzlLtp-4","title":"два\nреда"}`, true, http.StatusBadRequest},
		{`{"url":"https://youtu.be/YE7VzlLtp-4"}`, true, http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		req := httptest.NewRequest("POST", "/api/media/youtube", strings.NewReader(test.body))
		if test.authorized {
			req.AddCookie(cookies[0])
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != test.status {
			t.Errorf("%s: status %d, want %d, response %s", test.body, response.Code, test.status, response.Body)
		}
	}
}
