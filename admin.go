package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var assetName = regexp.MustCompile("^[0-9a-f]{64}\\.(wav|jpg)$")

func (a *App) adminRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/admin", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		accounts := []map[string]string{}
		for _, u := range a.state.Accounts {
			accounts = append(accounts, map[string]string{"name": u.Name, "role": u.Role})
		}
		tokens := []map[string]string{}
		for _, t := range a.state.Tokens {
			tokens = append(tokens, map[string]string{"id": t.ID, "name": t.Name, "role": t.Role})
		}
		hooks := []map[string]string{}
		for _, v := range a.state.Hooks {
			hooks = append(hooks, map[string]string{"id": v.ID, "kind": v.Kind})
		}
		respond(w, 200, map[string]any{"accounts": accounts, "tokens": tokens, "hooks": hooks, "audit": a.state.Audit})
	}))
	m.HandleFunc("POST /api/admin/{kind}", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		kind := r.PathValue("kind")
		if kind == "restore" {
			a.stageRestore(w, r)
			return
		}
		var v struct{ Name, Password, Role, Kind, URL, Secret string }
		if decode(r, &v) != nil {
			fail(w, 400, "Провери податке.")
			return
		}
		secret := ""
		err := a.change(user, "Администрација · "+kind, func(s *State) error {
			switch kind {
			case "users", "tokens":
				if !slices.Contains([]string{"admin", "dj", "editor", "viewer"}, v.Role) || strings.TrimSpace(v.Name) == "" || v.Name == a.user {
					return bad("Провери име и улогу.")
				}
				if kind == "users" {
					if len(v.Password) < 8 {
						return bad("Лозинка мора имати најмање осам знакова.")
					}
					if slices.ContainsFunc(s.Accounts, func(u Account) bool { return u.Name == v.Name }) {
						return bad("Име је заузето.")
					}
					salt := randomID()
					s.Accounts = append(s.Accounts, Account{v.Name, v.Role, salt, passHash(v.Password, salt)})
				} else {
					secret = randomID() + randomID()
					s.Tokens = append(s.Tokens, Token{randomID(), v.Name, v.Role, tokenHash(secret)})
				}
			case "hooks":
				u, err := url.Parse(v.URL)
				if err != nil || u.Host == "" || u.User != nil || !slices.Contains([]string{"https", "http"}, u.Scheme) || !slices.Contains([]string{"gotify", "discord"}, v.Kind) {
					return bad("Провери адресу и тип интеграције.")
				}
				s.Hooks = append(s.Hooks, Hook{randomID(), v.Kind, v.URL, v.Secret})
			default:
				return bad("Непозната радња.")
			}
			return nil
		})
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]string{"token": secret})
	}))
	m.HandleFunc("DELETE /api/admin/{kind}/{id}", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		id := r.PathValue("id")
		kind := r.PathValue("kind")
		err := a.change(user, "Уклоњено · "+kind, func(s *State) error {
			switch kind {
			case "users":
				s.Accounts = slices.DeleteFunc(s.Accounts, func(u Account) bool { return u.Name == id })
			case "tokens":
				s.Tokens = slices.DeleteFunc(s.Tokens, func(t Token) bool { return t.ID == id })
			case "hooks":
				s.Hooks = slices.DeleteFunc(s.Hooks, func(h Hook) bool { return h.ID == id })
			default:
				return bad("Непозната радња.")
			}
			return nil
		})
		if err == nil && kind == "users" {
			a.mu.Lock()
			for k, v := range a.sessions {
				if v.user == id {
					delete(a.sessions, k)
				}
			}
			a.mu.Unlock()
		}
		result(w, err)
	}))
	m.HandleFunc("GET /api/admin/backup", a.permit("admin", func(w http.ResponseWriter, r *http.Request, user string) {
		a.uploadMu.Lock()
		defer a.uploadMu.Unlock()
		a.mu.RLock()
		b, err := json.Marshal(a.state)
		media := slices.Clone(a.state.Media)
		a.mu.RUnlock()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="zlicast-backup.zip"`)
		w.Header().Set("Content-Type", "application/zip")
		z := zip.NewWriter(w)
		f, err := z.Create("state.json")
		if err != nil {
			return
		}
		if _, err = f.Write(b); err != nil {
			return
		}
		seen := map[string]bool{}
		for _, v := range media {
			if seen[v.Hash] {
				continue
			}
			seen[v.Hash] = true
			names := []string{v.Hash + ".wav"}
			if _, e := os.Stat(filepath.Join(a.data, "media", v.Hash+".jpg")); e == nil {
				names = append(names, v.Hash+".jpg")
			}
			for _, n := range names {
				src, e := os.Open(filepath.Join(a.data, "media", n))
				if e != nil {
					return
				}
				dst, e := z.Create("media/" + n)
				if e == nil {
					_, e = io.Copy(dst, src)
				}
				src.Close()
				if e != nil {
					return
				}
			}
		}
		z.Close()
	}))
}
func (a *App) notify(message string) {
	a.mu.RLock()
	hooks := slices.Clone(a.state.Hooks)
	a.mu.RUnlock()
	c := &http.Client{Timeout: 4 * time.Second}
	for _, h := range hooks {
		payload := map[string]string{"content": message}
		if h.Kind == "gotify" {
			payload = map[string]string{"title": "ZliCast", "message": message}
		}
		b, _ := json.Marshal(payload)
		req, e := http.NewRequest("POST", h.URL, strings.NewReader(string(b)))
		if e != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if h.Secret != "" && h.Kind == "gotify" {
			req.Header.Set("X-Gotify-Key", h.Secret)
		}
		resp, e := c.Do(req)
		if e == nil {
			resp.Body.Close()
		}
	}
}
func (a *App) stageRestore(w http.ResponseWriter, r *http.Request) {
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	tmp, err := os.CreateTemp(a.data, ".backup-")
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	path := tmp.Name()
	defer os.Remove(path)
	_, err = io.Copy(tmp, http.MaxBytesReader(w, r.Body, 16<<30))
	tmp.Close()
	if err == nil {
		_, err = inspectBackup(path)
	}
	if err != nil {
		fail(w, 400, "Архива је непотпуна или неисправна.")
		return
	}
	if _, err = os.Stat(filepath.Join(a.data, "restore.zip")); err == nil {
		fail(w, 409, "Враћање је већ заказано.")
		return
	}
	result(w, os.Rename(path, filepath.Join(a.data, "restore.zip")))
}
func inspectBackup(path string) (State, error) {
	var s State
	z, err := zip.OpenReader(path)
	if err != nil {
		return s, err
	}
	defer z.Close()
	files := map[string]bool{}
	var total uint64
	for _, f := range z.File {
		if files[f.Name] || f.UncompressedSize64 > 4<<30 {
			return s, bad("неисправна архива")
		}
		files[f.Name] = true
		total += f.UncompressedSize64
		if total > 32<<30 {
			return s, bad("превелика архива")
		}
		if f.Name == "state.json" {
			if f.UncompressedSize64 > 32<<20 {
				return s, bad("превелика конфигурација")
			}
			r, e := f.Open()
			if e != nil {
				return s, e
			}
			b, e := io.ReadAll(r)
			r.Close()
			if e != nil {
				return s, e
			}
			if e = json.Unmarshal(b, &s); e != nil {
				return s, e
			}
		} else if !strings.HasPrefix(f.Name, "media/") || !assetName.MatchString(strings.TrimPrefix(f.Name, "media/")) {
			return s, bad("непозната путања")
		}
	}
	if s.Version != 1 || s.History == nil || s.Samples == nil {
		return s, bad("некомпатибилна верзија")
	}
	slugs := map[string]bool{}
	for _, st := range s.Stations {
		if !goodSlug.MatchString(st.Slug) || slugs[st.Slug] || !slices.Contains([]string{"mp3", "aac"}, st.Format) || st.Bitrate < 64 || st.Bitrate > 320 {
			return s, bad("неисправна станица")
		}
		slugs[st.Slug] = true
	}
	for _, m := range s.Media {
		if !assetName.MatchString(m.Hash+".wav") || !files["media/"+m.Hash+".wav"] {
			return s, bad("недостаје снимак")
		}
	}
	return s, nil
}
func (a *App) restorePending() error {
	path := filepath.Join(a.data, "restore.zip")
	if _, e := os.Stat(path); errors.Is(e, os.ErrNotExist) {
		return nil
	}
	s, e := inspectBackup(path)
	if e != nil {
		return e
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		return e
	}
	defer z.Close()
	dir, e := os.MkdirTemp(a.data, ".restore-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	for _, f := range z.File {
		if f.Name == "state.json" {
			continue
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		target := filepath.Join(dir, filepath.Base(f.Name))
		dst, e := os.Create(target)
		if e == nil {
			_, e = io.Copy(dst, r)
			if ce := dst.Close(); e == nil {
				e = ce
			}
		}
		r.Close()
		if e != nil {
			return e
		}
	}
	for _, m := range s.Media {
		t := m.Track
		t.Path = filepath.Join(dir, m.Hash+".wav")
		d, e := openDeck(t)
		if e != nil {
			return e
		}
		d.close()
	}
	files, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	for _, f := range files {
		dst := filepath.Join(a.data, "media", f.Name())
		if _, e := os.Stat(dst); os.IsNotExist(e) {
			if e = os.Rename(filepath.Join(dir, f.Name()), dst); e != nil {
				return e
			}
		}
	}
	old := a.state
	a.state = s
	if e = a.saveLocked(); e != nil {
		a.state = old
		return e
	}
	z.Close()
	return os.Remove(path)
}
