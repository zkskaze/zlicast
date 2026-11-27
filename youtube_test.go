package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestYouTubeCookiesAreOptionalAndCopiedPerImport(t *testing.T) {
	dataDir, workDir := t.TempDir(), t.TempDir()
	if present, err := stageYouTubeCookies(dataDir, workDir); present || err != nil {
		t.Fatalf("anonymous import: present %v, error %v", present, err)
	}
	source := filepath.Join(dataDir, youtubeCookiesFile)
	if err := os.WriteFile(source, []byte("invalid export"), 0600); err != nil {
		t.Fatal(err)
	}
	if present, err := stageYouTubeCookies(dataDir, workDir); present || err == nil {
		t.Fatalf("invalid cookie export: present %v, error %v", present, err)
	}
	const fixture = "# Netscape HTTP Cookie File\r\n.youtube.com\tTRUE\t/\tTRUE\t0\tfixture\tsynthetic\r\n"
	if err := os.WriteFile(source, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if present, err := stageYouTubeCookies(dataDir, workDir); !present || err != nil {
		t.Fatalf("authenticated import: present %v, error %v", present, err)
	}
	staged := filepath.Join(workDir, youtubeCookiesFile)
	if raw, err := os.ReadFile(staged); string(raw) != fixture || err != nil {
		t.Fatalf("copied cookie jar: %s, error %v", raw, err)
	}
	if err := os.WriteFile(staged, []byte("yt-dlp updated its cookie jar"), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(source); string(raw) != fixture || err != nil {
		t.Fatalf("source cookie jar changed: %s, error %v", raw, err)
	}
}

func TestYouTubeSignInError(t *testing.T) {
	tests := []struct {
		details    string
		hasCookies bool
		want       string
	}{
		{"ERROR: [youtube] 2FJlbPqka3M: Sign in to confirm you’re not a bot.", false, "data/youtube-cookies.txt"},
		{"ERROR: [youtube] Sign in to confirm you're not a bot.", true, "Освежи YouTube колачиће"},
		{"WARNING: The provided YouTube account cookies are no longer valid.", true, "више не важе"},
		{"ERROR: yt-dlp failed at /private/path", false, "Детаљи су у журналу сервера"},
	}
	for _, test := range tests {
		var diagnostic youtubeDiagnostic
		_, _ = diagnostic.Write([]byte(strings.Repeat("x", 10000)))
		for _, part := range []string{test.details[:12], test.details[12:]} {
			_, _ = diagnostic.Write([]byte(part))
		}
		got := youtubeDownloadError(diagnostic.tail, test.hasCookies).Error()
		if !strings.Contains(got, test.want) || strings.Contains(got, "/private/path") {
			t.Errorf("%q: got %q, want %q", test.details, got, test.want)
		}
	}
}
