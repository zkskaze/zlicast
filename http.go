package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var goodSlug = regexp.MustCompile("^[a-z0-9][a-z0-9-]{0,47}$")
var goodColor = regexp.MustCompile("^#[0-9a-fA-F]{6}$")
var hlsSegment = regexp.MustCompile("^index[0-9]+\\.ts$")

func sameOrigin(r *http.Request) bool {
	u, e := url.Parse(r.Header.Get("Origin"))
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && strings.EqualFold(u.Host, r.Host)
}
func equal(a, b string) bool {
	x := sha256.Sum256([]byte(a))
	y := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func tokenHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func passHash(s, salt string) string {
	b, _ := pbkdf2.Key(sha256.New, s, []byte(salt), 150000, 32)
	return hex.EncodeToString(b)
}

const browserSessionTTL = 30 * 24 * time.Hour

func sessionTag(token, credential string) string {
	sum := hmac.New(sha256.New, []byte(token))
	sum.Write([]byte(credential))
	return hex.EncodeToString(sum.Sum(nil))
}

func (a *App) accountLocked(name string) (role, credential string) {
	if name == a.user {
		return "admin", "admin\x00" + a.password
	}
	for _, u := range a.state.Accounts {
		if u.Name == name {
			return u.Role, "account\x00" + u.Hash
		}
	}
	return "", ""
}

func (a *App) identity(r *http.Request) (string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		v := tokenHash(b)
		for _, t := range a.state.Tokens {
			if equal(t.Hash, v) {
				return t.Name, t.Role
			}
		}
		return "", ""
	}
	c, err := r.Cookie("zlicast")
	if err != nil {
		return "", ""
	}
	s, ok := a.sessions[tokenHash(c.Value)]
	if !ok || !s.Expires.After(time.Now()) {
		return "", ""
	}
	role, credential := a.accountLocked(s.User)
	if role == "" || !equal(s.Tag, sessionTag(c.Value, credential)) {
		return "", ""
	}
	return s.User, role
}

type endpoint func(http.ResponseWriter, *http.Request, string)

func (a *App) permit(roles string, fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, role := a.identity(r)
		if role == "" {
			fail(w, 401, "Пријави се.")
			return
		}
		if role != "admin" && roles != "*" && !slices.Contains(strings.Split(roles, ","), role) {
			fail(w, 403, "Налог нема дозволу за ову радњу.")
			return
		}
		fn(w, r, u)
	}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var v struct{ User, Password string }
	if decode(r, &v) != nil {
		fail(w, 400, "Провери податке.")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	a.mu.Lock()
	times := slices.DeleteFunc(a.attempts[ip], func(t time.Time) bool { return now.Sub(t) > time.Minute })
	if len(times) >= 12 {
		a.mu.Unlock()
		fail(w, 429, "Покушај поново за минут.")
		return
	}
	a.attempts[ip] = append(times, now)
	accounts := slices.Clone(a.state.Accounts)
	a.mu.Unlock()
	credential := ""
	if equal(v.User, a.user) && equal(v.Password, a.password) {
		credential = "admin\x00" + a.password
	} else {
		for _, u := range accounts {
			if equal(u.Name, v.User) && equal(u.Hash, passHash(v.Password, u.Salt)) {
				credential = "account\x00" + u.Hash
				break
			}
		}
	}
	if credential == "" {
		fail(w, 401, "Погрешно корисничко име или лозинка.")
		return
	}
	id := randomID() + randomID()
	a.mu.Lock()
	currentRole, currentCredential := a.accountLocked(v.User)
	if currentRole == "" || !equal(credential, currentCredential) {
		a.mu.Unlock()
		fail(w, 401, "Пријава више није важећа.")
		return
	}
	previous := maps.Clone(a.sessions)
	for k, s := range a.sessions {
		if !s.Expires.After(now) {
			delete(a.sessions, k)
		}
	}
	if cookie, err := r.Cookie("zlicast"); err == nil {
		delete(a.sessions, tokenHash(cookie.Value))
	}
	a.sessions[tokenHash(id)] = session{v.User, now.Add(browserSessionTTL), sessionTag(id, credential)}
	if err := a.saveSessionsLocked(); err != nil {
		a.sessions = previous
		a.mu.Unlock()
		fail(w, 500, "Пријаву тренутно није могуће сачувати.")
		return
	}
	delete(a.attempts, ip)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "zlicast", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(browserSessionTTL.Seconds()), Expires: now.Add(browserSessionTTL)})
	respond(w, 200, map[string]string{"user": v.User, "role": currentRole})
}

type stationView struct {
	Station
	Status   Status  `json:"status"`
	Upcoming []Track `json:"upcoming,omitempty"`
}

func (a *App) coreRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /hls/{slug}/{file}", func(w http.ResponseWriter, r *http.Request) {
		e := a.engine(r.PathValue("slug"))
		file := r.PathValue("file")
		if e == nil || (file != "index.m3u8" && !hlsSegment.MatchString(file)) {
			http.NotFound(w, r)
			return
		}
		e.mu.Lock()
		dir := e.hlsDir
		e.mu.Unlock()
		if dir == "" {
			http.NotFound(w, r)
			return
		}
		if file == "index.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Content-Type", "video/mp2t")
		}
		http.ServeFile(w, r, filepath.Join(dir, file))
	})
	m.HandleFunc("GET /api/system", a.permit("*", func(w http.ResponseWriter, r *http.Request, user string) {
		cpu, ram, free := systemValues(a.data)
		up := time.Since(a.started).Seconds()
		a.mu.RLock()
		seen := map[string]bool{}
		var size int64
		for _, v := range a.state.Media {
			if !seen[v.Hash] {
				size += int64(v.Duration * 192000)
				seen[v.Hash] = true
			}
		}
		a.mu.RUnlock()
		respond(w, 200, map[string]any{"uptime": int(up), "ram": ram, "diskFree": free, "mediaBytes": size, "cpu": 100 * cpu / max(1, up)})
	}))
	m.HandleFunc("POST /api/login", a.login)
	m.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("zlicast"); err == nil {
			a.mu.Lock()
			key := tokenHash(c.Value)
			if s, ok := a.sessions[key]; ok {
				delete(a.sessions, key)
				if err := a.saveSessionsLocked(); err != nil {
					a.sessions[key] = s
					a.mu.Unlock()
					fail(w, 500, "Одјаву тренутно није могуће сачувати.")
					return
				}
			}
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "zlicast", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		respond(w, 200, true)
	})
	m.HandleFunc("GET /api/session", a.permit("*", func(w http.ResponseWriter, r *http.Request, user string) {
		_, role := a.identity(r)
		respond(w, 200, map[string]string{"user": user, "role": role})
	}))
	m.HandleFunc("GET /api/dashboard", a.permit("*", func(w http.ResponseWriter, r *http.Request, user string) {
		_, role := a.identity(r)
		a.mu.Lock()
		defer a.mu.Unlock()
		list := []stationView{}
		for _, s := range a.state.Stations {
			v := stationView{Station: s, Upcoming: a.upcomingLocked(s)}
			if e := a.engines[s.Slug]; e != nil {
				v.Status = e.snapshot()
			}
			list = append(list, v)
		}
		respond(w, 200, map[string]any{"user": user, "role": role, "stations": list, "media": a.state.Media, "events": a.state.Events, "playlists": a.state.Playlists, "timezone": a.loc.String()})
	}))
	m.HandleFunc("PATCH /api/stations/{slug}/jingle", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		var rule struct {
			ID    string `json:"id"`
			Every int    `json:"every"`
		}
		if decode(r, &rule) != nil || rule.Every < 0 || rule.Every > 1000 {
			fail(w, 400, "Унеси број песама од 0 до 1000.")
			return
		}
		slug := r.PathValue("slug")
		err := a.change(user, "Џингл по броју песама · "+slug, func(s *State) error {
			i := slices.IndexFunc(s.Stations, func(st Station) bool { return st.Slug == slug })
			if i < 0 {
				return bad("Непозната станица.")
			}
			if rule.ID != "" {
				m, ok := findMedia(s, rule.ID)
				if !ok || m.Category != "jingles" || !a.available(m, slug, time.Now()) {
					return bad("Џингл није доступан за ову станицу.")
				}
			}
			s.Stations[i].Rules.JingleSongs = rule.Every
			s.Stations[i].Rules.JingleID = rule.ID
			return nil
		})
		result(w, err)
	}))
	m.HandleFunc("POST /api/stations", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		var s Station
		if decode(r, &s) != nil || !goodSlug.MatchString(s.Slug) || strings.TrimSpace(s.Name) == "" || len(s.Name) > 100 {
			fail(w, 400, "Унеси назив и кратку адресу од малих латиничних слова, цифара и цртица.")
			return
		}
		if s.Format == "" {
			s.Format = "mp3"
		}
		s.Description = strings.TrimSpace(s.Description)
		s.Website = strings.TrimSpace(s.Website)
		s.Owner = strings.TrimSpace(s.Owner)
		if utf8.RuneCountInString(s.Description) > 3000 || utf8.RuneCountInString(s.Owner) > 120 || len(s.Website) > 2048 {
			fail(w, 400, "Опис може да има до 3000 знакова, а име власника до 120.")
			return
		}
		if s.Website != "" {
			address, err := url.Parse(s.Website)
			if err != nil || (address.Scheme != "http" && address.Scheme != "https") || address.Hostname() == "" {
				fail(w, 400, "Сајт станице мора да буде важећа http:// или https:// адреса.")
				return
			}
		}
		if s.Bitrate == 0 {
			s.Bitrate = 128
		}
		if !slices.Contains([]string{"mp3", "aac"}, s.Format) || !slices.Contains([]int{64, 96, 128, 160, 192, 256, 320}, s.Bitrate) || s.Crossfade < 0 || s.Crossfade > 15 {
			fail(w, 400, "Провери формат, битрејт и прелаз (0–15 секунди).")
			return
		}
		if !goodColor.MatchString(s.Color) {
			s.Color = "#c6f479"
		}
		if s.Logo != "" && !validLogo(s.Logo) {
			fail(w, 400, "Лого мора бити PNG, JPEG или WebP до 256 KiB.")
			return
		}
		a.uploadMu.Lock()
		defer a.uploadMu.Unlock()
		err := a.change(user, "Станица · "+s.Name, func(st *State) error {
			for i, v := range st.Stations {
				if v.Slug == s.Slug {
					st.Stations[i] = s
					return nil
				}
			}
			st.Stations = append(st.Stations, s)
			return nil
		})
		if err == nil {
			a.startStation(s)
		}
		result(w, err)
	}))
	m.HandleFunc("DELETE /api/stations/{slug}", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		slug := r.PathValue("slug")
		a.uploadMu.Lock()
		defer a.uploadMu.Unlock()
		err := a.change(user, "Уклоњена станица · "+slug, func(s *State) error {
			if !hasStation(s, slug) {
				return bad("Непозната станица.")
			}
			s.Stations = slices.DeleteFunc(s.Stations, func(v Station) bool { return v.Slug == slug })
			s.Events = slices.DeleteFunc(s.Events, func(v Event) bool { return v.Station == slug })
			for i := range s.Media {
				if s.Media[i].Station == slug {
					s.Media[i].Station = ""
				}
			}
			for i := range s.Playlists {
				if s.Playlists[i].Station == slug {
					s.Playlists[i].Station = ""
				}
			}
			delete(s.History, slug)
			delete(s.Samples, slug)
			return nil
		})
		if err == nil {
			a.mu.Lock()
			e := a.engines[slug]
			delete(a.engines, slug)
			delete(a.rotate, slug)
			a.mu.Unlock()
			if e != nil {
				e.close()
			}
		}
		result(w, err)
	}))
	m.HandleFunc("GET /stream/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug, extension, _ := strings.Cut(r.PathValue("slug"), ".")
		e := a.engine(slug)
		if e == nil {
			http.NotFound(w, r)
			return
		}
		if extension != "" && extension != e.getConfig().Format {
			http.NotFound(w, r)
			return
		}
		e.stream(w, r)
	})
	m.HandleFunc("GET /stream/{slug}/pcm", func(w http.ResponseWriter, r *http.Request) {
		e := a.engine(r.PathValue("slug"))
		if e == nil {
			http.NotFound(w, r)
			return
		}
		e.serveAudio(w, r, true)
	})
	m.HandleFunc("GET /listen/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSuffix(r.PathValue("slug"), ".m3u")
		e := a.engine(slug)
		if e == nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.PathValue("slug"), ".m3u") {
			scheme := "http"
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				scheme = "https"
			}
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Query().Get("download") == "1" {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", slug+".m3u"))
			}
			config := e.getConfig()
			address := url.URL{Scheme: scheme, Host: r.Host, Path: "/stream/" + slug + "." + config.Format}
			name := strings.NewReplacer("\r", " ", "\n", " ").Replace(config.Name)
			fmt.Fprintf(w, "#EXTM3U\r\n#EXTINF:-1,%s\r\n%s\r\n", name, address.String())
			return
		}
		b, _ := assets.ReadFile("web/build/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	m.HandleFunc("GET /api/stations/{slug}/{view}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		a.mu.RLock()
		defer a.mu.RUnlock()
		e := a.engines[slug]
		if e == nil {
			fail(w, 404, "Непозната станица.")
			return
		}
		status := e.snapshot()
		switch r.PathValue("view") {
		case "now-playing":
			var st Station
			for _, v := range a.state.Stations {
				if v.Slug == slug {
					st = v
					break
				}
			}
			respond(w, 200, stationView{Station: st, Status: status})
		case "history":
			respond(w, 200, a.state.History[slug])
		case "stats":
			respond(w, 200, struct {
				Status
				Samples []Sample `json:"samples"`
			}{status, a.state.Samples[slug]})
		default:
			http.NotFound(w, r)
		}
	})
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		air := 0
		for _, e := range a.engines {
			if e.snapshot().OnAir {
				air++
			}
		}
		code := 200
		if air < len(a.engines) {
			code = 503
		}
		respond(w, code, map[string]any{"ok": code == 200, "onAir": air, "stations": len(a.engines), "uptime": int(time.Since(a.started).Seconds())})
	})
}
func validLogo(s string) bool {
	return len(s) <= 350000 && (strings.HasPrefix(s, "data:image/png;base64,") || strings.HasPrefix(s, "data:image/jpeg;base64,") || strings.HasPrefix(s, "data:image/webp;base64,"))
}
