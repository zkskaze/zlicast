package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

func (a *App) program(s *State, slug, kind, value string, now time.Time) []Track {
	var out []Track
	add := func(id string) {
		if m, ok := findMedia(s, id); ok && a.available(m, slug, now) {
			out = append(out, a.track(m))
		}
	}
	switch kind {
	case "track":
		add(value)
	case "playlist":
		for _, p := range s.Playlists {
			if p.ID == value && (p.Station == "" || p.Station == slug) {
				for _, id := range p.Tracks {
					add(id)
				}
				break
			}
		}
	case "category":
		for _, m := range s.Media {
			if m.Category == value && a.available(m, slug, now) {
				out = append(out, a.track(m))
			}
		}
	}
	return out
}
func matchEvent(e Event, now time.Time) bool {
	if e.Time != now.Format("15:04") || e.Date > now.Format("2006-01-02") {
		return false
	}
	date, err := time.ParseInLocation("2006-01-02", e.Date, now.Location())
	if err != nil {
		return false
	}
	switch e.Repeat {
	case "daily":
		return true
	case "weekly":
		return now.Weekday() == date.Weekday()
	case "monthly":
		return now.Day() == date.Day()
	default:
		return e.Date == now.Format("2006-01-02")
	}
}
func (a *App) tick(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now = now.In(a.loc)
	key := now.Format("2006-01-02 15:04")
	var idx []int
	for i, e := range a.state.Events {
		if e.Last != key && matchEvent(e, now) {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(i, j int) bool { return a.state.Events[idx[i]].Priority > a.state.Events[idx[j]].Priority })
	used := map[string]bool{}
	for _, i := range idx {
		e := &a.state.Events[i]
		e.Last = key
		if used[e.Station] {
			continue
		}
		en := a.engines[e.Station]
		if en == nil {
			continue
		}
		if e.Target == "live" {
			used[e.Station] = true
			continue
		}
		p := a.program(&a.state, e.Station, e.Target, e.Value, now)
		if len(p) > 0 {
			p = append(a.program(&a.state, e.Station, "track", e.Intro, now), p...)
			p = append(p, a.program(&a.state, e.Station, "track", e.Outro, now)...)
			used[e.Station] = en.queue(p, e.Interrupt)
		}
	}
	if len(idx) > 0 {
		if err := a.saveLocked(); err != nil {
			fmt.Println(err)
		}
	}
}
func (a *App) clock(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.tick(now)
			minute := now.Format("200601021504")
			if minute != last {
				last = minute
				a.mu.Lock()
				for slug, e := range a.engines {
					s := e.snapshot()
					h := append(a.state.Samples[slug], Sample{now, s.Listeners, s.OnAir})
					a.state.Samples[slug] = h[max(0, len(h)-1440):]
				}
				if err := a.saveLocked(); err != nil {
					fmt.Println(err)
				}
				a.mu.Unlock()
			}
		}
	}
}
func (a *App) automationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/stations/{slug}/play", a.permit("admin,dj,editor", func(w http.ResponseWriter, r *http.Request, user string) {
		var v struct {
			ID        string
			Interrupt bool
		}
		if decode(r, &v) != nil {
			fail(w, 400, "Провери избор снимка.")
			return
		}
		slug := r.PathValue("slug")
		a.mu.RLock()
		p := a.program(&a.state, slug, "track", v.ID, time.Now())
		e := a.engines[slug]
		a.mu.RUnlock()
		if e == nil || len(p) == 0 || !e.queue(p, v.Interrupt) {
			fail(w, 409, "Снимак није доступан за ову станицу.")
			return
		}
		respond(w, 200, true)
	}))
	mux.HandleFunc("POST /api/playlists", a.permit("admin,editor", func(w http.ResponseWriter, r *http.Request, user string) {
		var p Playlist
		if decode(r, &p) != nil || strings.TrimSpace(p.Name) == "" || len(p.Tracks) == 0 {
			fail(w, 400, "Унеси име и додај нумере.")
			return
		}
		if p.ID == "" {
			p.ID = randomID()
		}
		result(w, a.change(user, "Плејлиста · "+p.Name, func(s *State) error {
			if p.Station != "" && !hasStation(s, p.Station) {
				return bad("Непозната станица.")
			}
			for _, id := range p.Tracks {
				m, ok := findMedia(s, id)
				if !ok || m.Station != "" && m.Station != p.Station {
					return bad("Нумера не припада библиотеци.")
				}
			}
			i := slices.IndexFunc(s.Playlists, func(v Playlist) bool { return v.ID == p.ID })
			if i < 0 {
				s.Playlists = append(s.Playlists, p)
			} else {
				s.Playlists[i] = p
			}
			return nil
		}))
	}))
	mux.HandleFunc("POST /api/events", a.permit("admin,editor,dj", func(w http.ResponseWriter, r *http.Request, user string) {
		var e Event
		if decode(r, &e) != nil || strings.TrimSpace(e.Name) == "" {
			fail(w, 400, "Унеси назив термина.")
			return
		}
		if _, err := time.Parse("15:04", e.Time); err != nil {
			fail(w, 400, "Провери време.")
			return
		}
		if _, err := time.Parse("2006-01-02", e.Date); err != nil || !slices.Contains([]string{"once", "daily", "weekly", "monthly"}, e.Repeat) {
			fail(w, 400, "Провери датум и понављање.")
			return
		}
		if e.ID == "" {
			e.ID = randomID()
		}
		result(w, a.change(user, "Термин · "+e.Name, func(s *State) error {
			if !hasStation(s, e.Station) {
				return bad("Непозната станица.")
			}
			if e.Target != "live" && len(a.program(s, e.Station, e.Target, e.Value, time.Now())) == 0 {
				return bad("Извор програма је празан или недоступан.")
			}
			i := slices.IndexFunc(s.Events, func(v Event) bool { return v.ID == e.ID })
			if i < 0 {
				s.Events = append(s.Events, e)
			} else {
				e.Last = s.Events[i].Last
				s.Events[i] = e
			}
			return nil
		}))
	}))
	for _, kind := range []string{"events", "playlists"} {
		mux.HandleFunc("DELETE /api/"+kind+"/{id}", a.permit("admin,editor", func(w http.ResponseWriter, r *http.Request, user string) {
			id := r.PathValue("id")
			result(w, a.change(user, "Уклоњено · "+kind, func(s *State) error {
				if kind == "events" {
					s.Events = slices.DeleteFunc(s.Events, func(e Event) bool { return e.ID == id })
				} else {
					s.Playlists = slices.DeleteFunc(s.Playlists, func(p Playlist) bool { return p.ID == id })
					s.Events = slices.DeleteFunc(s.Events, func(e Event) bool { return e.Target == "playlist" && e.Value == id })
				}
				return nil
			}))
		}))
	}
}
