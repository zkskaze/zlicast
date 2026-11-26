package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserSessionSurvivesRestartAndCanBeRevoked(t *testing.T) {
	dir := t.TempDir()
	open := func(password string) *App {
		t.Helper()
		app, err := newApp(dir, "admin", password, "", time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	call := func(app *App, method, route, body string, cookie *http.Cookie, expected int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, route, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		app.routes().ServeHTTP(rec, req)
		if rec.Code != expected {
			t.Fatalf("%s %s: status %d, want %d", method, route, rec.Code, expected)
		}
		return rec
	}
	login := func(app *App) *http.Cookie {
		return call(app, "POST", "/api/login", `{"user":"admin","password":"password"}`, nil, 200).Result().Cookies()[0]
	}

	app := open("password")
	cookie := login(app)
	if cookie.MaxAge != int(browserSessionTTL.Seconds()) || !cookie.HttpOnly {
		t.Fatal("login cookie is not persistent or HttpOnly")
	}
	saved, err := app.db.read("SELECT body FROM browser_sessions WHERE id=1")
	if err != nil || !strings.Contains(string(saved), tokenHash(cookie.Value)) || strings.Contains(string(saved), cookie.Value) {
		t.Fatal("SQLite should store only a hash of the cookie")
	}
	app.stop()

	app = open("password")
	call(app, "GET", "/api/session", "", cookie, 200)
	call(app, "POST", "/api/logout", "", cookie, 200)
	second := login(app)
	app.stop()

	app = open("password")
	call(app, "GET", "/api/session", "", cookie, 401)
	call(app, "GET", "/api/session", "", second, 200)
	app.stop()

	app = open("changed")
	defer app.stop()
	call(app, "GET", "/api/session", "", second, 401)
}
