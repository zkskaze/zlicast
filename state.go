package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	randv2 "math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Rules struct {
	JingleSongs   int    `json:"jingleSongs"`
	JingleID      string `json:"jingleId"`
	JingleMinutes int    `json:"jingleMinutes"`
	IDHourly      bool   `json:"idHourly"`
	AdMinutes     int    `json:"adMinutes"`
	RepeatSong    int    `json:"repeatSong"`
	RepeatArtist  int    `json:"repeatArtist"`
	Category      string `json:"category"`
}
type Station struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Website     string `json:"website,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Format      string `json:"format"`
	Bitrate     int    `json:"bitrate"`
	Crossfade   int    `json:"crossfade"`
	Logo        string `json:"logo"`
	Color       string `json:"color"`
	Rules       Rules  `json:"rules"`
	HLS         bool   `json:"hls"`
}

func (s Station) config() ProgramConfig {
	return ProgramConfig{s.Name, s.Format, s.Bitrate, s.Crossfade, s.HLS}
}

func (s Station) rotationCategory() string {
	if s.Rules.Category == "" {
		return "music"
	}
	return s.Rules.Category
}

type Media struct {
	Track
	Station string   `json:"station"`
	Tags    []string `json:"tags"`
	Hash    string   `json:"hash"`
	Cover   string   `json:"cover"`
	Expires string   `json:"expires"`
}
type Playlist struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Station string   `json:"station"`
	Tracks  []string `json:"tracks"`
}
type Event struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Station   string `json:"station"`
	Time      string `json:"time"`
	Date      string `json:"date"`
	Repeat    string `json:"repeat"`
	Target    string `json:"target"`
	Value     string `json:"value"`
	Interrupt bool   `json:"interrupt"`
	Priority  int    `json:"priority"`
	Intro     string `json:"intro"`
	Outro     string `json:"outro"`
	Last      string `json:"last,omitempty"`
}
type Entry struct {
	At time.Time `json:"at"`
	Track
}
type Sample struct {
	At        time.Time `json:"at"`
	Listeners int       `json:"listeners"`
	OnAir     bool      `json:"onAir"`
}
type Audit struct {
	At     time.Time `json:"at"`
	User   string    `json:"user"`
	Action string    `json:"action"`
}
type Account struct{ Name, Role, Salt, Hash string }
type Token struct{ ID, Name, Role, Hash string }
type Hook struct{ ID, Kind, URL, Secret string }
type State struct {
	Version   int
	Stations  []Station
	Media     []Media
	Playlists []Playlist
	Events    []Event
	History   map[string][]Entry
	Samples   map[string][]Sample
	Audit     []Audit
	Accounts  []Account
	Tokens    []Token
	Hooks     []Hook
}
type session struct {
	User    string
	Expires time.Time
	Tag     string
}
type rotation struct {
	songs      int
	jingle, ad time.Time
	hour       string
	category   string
	rank       map[string]uint64
	played     map[string]bool
	upcoming   []string
}
type App struct {
	mu                           sync.RWMutex
	state                        State
	db                           *database
	data, user, password, source string
	loc                          *time.Location
	engines                      map[string]*Engine
	rotate                       map[string]*rotation
	sessions                     map[string]session
	attempts                     map[string][]time.Time
	started                      time.Time
	uploadMu                     sync.Mutex
	imports                      mediaImports
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func initialState() State {
	defaults := func(slug, name, color string, b int) Station {
		return Station{Slug: slug, Name: name, Color: color, Format: "mp3", Bitrate: b, Crossfade: 3, Rules: Rules{JingleSongs: 3, RepeatSong: 180, RepeatArtist: 45}}
	}
	return State{Version: 1, Stations: []Station{defaults("zlifm", "ZliFM", "#c6f479", 128), defaults("kaze", "Radio Kaze", "#a1d6d2", 192)}, Media: []Media{}, Playlists: []Playlist{}, Events: []Event{}, History: map[string][]Entry{}, Samples: map[string][]Sample{}}
}
func newApp(data, user, password, source string, loc *time.Location) (*App, error) {
	if err := os.MkdirAll(filepath.Join(data, "media"), 0700); err != nil {
		return nil, err
	}
	db, err := openDatabase(filepath.Join(data, "zlicast.sqlite"))
	if err != nil {
		return nil, err
	}
	a := &App{db: db, data: data, user: user, password: password, source: source, loc: loc, engines: map[string]*Engine{}, rotate: map[string]*rotation{}, sessions: map[string]session{}, attempts: map[string][]time.Time{}, started: time.Now()}
	if err = db.exec("PRAGMA busy_timeout=2000; PRAGMA locking_mode=EXCLUSIVE; CREATE TABLE IF NOT EXISTS config(id INTEGER PRIMARY KEY,body TEXT NOT NULL); CREATE TABLE IF NOT EXISTS browser_sessions(id INTEGER PRIMARY KEY,body TEXT NOT NULL)"); err != nil {
		db.close()
		return nil, err
	}
	b, err := db.read("SELECT body FROM config WHERE id=1")
	if err == nil {
		if len(b) > 0 {
			err = json.Unmarshal(b, &a.state)
			if err == nil && a.state.Version != 1 {
				err = errors.New("непозната верзија базе")
			}
		} else {
			a.state = initialState()
			err = a.saveLocked()
		}
	}
	if err != nil {
		db.close()
		return nil, err
	}
	sessions, err := db.read("SELECT body FROM browser_sessions WHERE id=1")
	if err == nil && len(sessions) > 0 {
		err = json.Unmarshal(sessions, &a.sessions)
	}
	if err != nil {
		db.close()
		return nil, err
	}
	return a, nil
}
func (a *App) saveSessionsLocked() error {
	body, err := json.Marshal(a.sessions)
	if err != nil {
		return err
	}
	return a.db.exec("INSERT OR REPLACE INTO browser_sessions(id,body) VALUES(1,'" + strings.ReplaceAll(string(body), "'", "''") + "')")
}
func (a *App) saveLocked() error {
	b, e := json.Marshal(a.state)
	if e != nil {
		return e
	}
	return a.db.exec("INSERT OR REPLACE INTO config(id,body) VALUES(1,'" + strings.ReplaceAll(string(b), "'", "''") + "')")
}
func (a *App) change(user, action string, fn func(*State) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var draft State
	b, err := json.Marshal(a.state)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &draft); err != nil {
		return err
	}
	if err = fn(&draft); err != nil {
		return err
	}
	draft.Audit = append(draft.Audit, Audit{time.Now(), user, action})
	if len(draft.Audit) > 500 {
		draft.Audit = draft.Audit[len(draft.Audit)-500:]
	}
	old := a.state
	a.state = draft
	if err = a.saveLocked(); err != nil {
		a.state = old
	}
	return err
}
func (a *App) mediaPath(m Media) string { return filepath.Join(a.data, "media", m.Hash+".wav") }
func (a *App) track(m Media) Track      { t := m.Track; t.Path = a.mediaPath(m); return t }
func (a *App) available(m Media, slug string, now time.Time) bool {
	return (m.Station == "" || m.Station == slug) && (m.Expires == "" || m.Expires >= now.In(a.loc).Format("2006-01-02"))
}

// Time tags only restrict automatic jingle rotation. Explicit cues always win.
func jingleTime(tags []string, local time.Time) bool {
	window, matchesWindow, date, matchesDate := false, false, false, false
	for _, tag := range tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "јутро":
			window = true
			matchesWindow = matchesWindow || local.Hour() >= 5 && local.Hour() < 12
		case "вече":
			window = true
			matchesWindow = matchesWindow || local.Hour() >= 18
		case "ноћ":
			window = true
			matchesWindow = matchesWindow || local.Hour() < 5
		default:
			if day, ok := strings.CutPrefix(tag, "датум:"); ok {
				date = true
				matchesDate = matchesDate || day == local.Format("2006-01-02") || day == local.Format("01-02")
			}
		}
	}
	return (!window || matchesWindow) && (!date || matchesDate)
}
func (a *App) engine(slug string) *Engine { a.mu.RLock(); defer a.mu.RUnlock(); return a.engines[slug] }
func (a *App) startStation(s Station) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.engines[s.Slug]; e != nil {
		e.configure(s.config())
		return
	}
	now := time.Now()
	a.rotate[s.Slug] = &rotation{
		jingle: now, ad: now, hour: now.In(a.loc).Format("2006-01-02T15"),
		category: s.rotationCategory(), played: make(map[string]bool),
	}
	a.engines[s.Slug] = newEngine(s.config(), func() Track { return a.next(s.Slug) }, func(t Track) { a.onPlay(s.Slug, t) })
}
func (a *App) onPlay(slug string, t Track) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.engines[slug]
	if e == nil {
		return
	}
	if m, ok := findMedia(&a.state, t.ID); ok && m.Title != t.Title {
		t.Title = m.Title
		e.updateTrackTitle(t.ID, t.Title)
	}
	h := append([]Entry{{time.Now(), t}}, a.state.History[slug]...)
	if len(a.state.Hooks) > 0 && (t.Category == "live" || len(h) > 1 && h[1].Category == "live") {
		go a.notify(slug + " · " + t.Title)
	}
	a.state.History[slug] = h[:min(500, len(h))]
	if t.Category == "music" {
		a.rotate[slug].songs++
	} else if t.Category == "jingles" {
		rot := a.rotate[slug]
		now := time.Now()
		rot.songs = 0
		rot.jingle = now
		rot.hour = now.In(a.loc).Format("2006-01-02T15")
	}
	if rot := a.rotate[slug]; !t.autoRotation && t.Category == rot.category && rot.played != nil {
		rot.played[t.ID] = true
	}
	if err := a.saveLocked(); err != nil {
		log.Println(err)
	}
}
func (a *App) next(slug string) Track {
	a.mu.Lock()
	defer a.mu.Unlock()
	var s Station
	for _, v := range a.state.Stations {
		if v.Slug == slug {
			s = v
			break
		}
	}
	rot := a.rotate[slug]
	if rot == nil {
		return Track{}
	}
	now := time.Now()
	r := s.Rules
	category := s.rotationCategory()
	hour := now.In(a.loc).Format("2006-01-02T15")
	jingleByCount := r.JingleSongs > 0 && rot.songs >= r.JingleSongs
	jingleByTime := r.IDHourly && rot.hour != hour || r.JingleMinutes > 0 && now.Sub(rot.jingle) >= time.Duration(r.JingleMinutes)*time.Minute
	ad := r.AdMinutes > 0 && now.Sub(rot.ad) >= time.Duration(r.AdMinutes)*time.Minute
	candidates := func(cat string) []Media {
		var out []Media
		for _, m := range a.state.Media {
			if m.Category == cat && a.available(m, slug, now) && (cat != "jingles" || jingleTime(m.Tags, now.In(a.loc))) {
				out = append(out, m)
			}
		}
		return out
	}
	if jingleByCount && r.JingleID != "" {
		if m, ok := findMedia(&a.state, r.JingleID); ok && m.Category == "jingles" && a.available(m, slug, now) {
			return a.track(m)
		}
	}
	if jingleByTime || jingleByCount && r.JingleID == "" {
		if list := candidates("jingles"); len(list) > 0 {
			return a.track(list[randv2.IntN(len(list))])
		}
	}
	if ad {
		if list := candidates("ads"); len(list) > 0 {
			rot.ad = now
			return a.track(list[randv2.IntN(len(list))])
		}
	}
	list := candidates(category)
	if len(list) == 0 {
		return Track{}
	}
	upcoming := rot.plan(category, list, a.state.History[slug], r, now)
	chosen := upcoming[0]
	rot.upcoming = rot.upcoming[1:]
	rot.played[chosen.ID] = true
	track := a.track(chosen)
	track.autoRotation = true
	return track
}

func (rot *rotation) prepare(category string, list []Media) {
	if rot.category != category || rot.played == nil {
		rot.category = category
		rot.played = make(map[string]bool)
		rot.rank = nil
		rot.upcoming = nil
	}
	if rot.rank == nil {
		rot.rank = make(map[string]uint64)
	}
	if !slices.ContainsFunc(list, func(m Media) bool { return !rot.played[m.ID] }) {
		clear(rot.played)
		clear(rot.rank)
		rot.upcoming = nil
	}
}

func (rot *rotation) plan(category string, list []Media, history []Entry, rules Rules, now time.Time) []Media {
	rot.prepare(category, list)
	remaining := make(map[string]Media, len(list))
	for _, m := range list {
		if !rot.played[m.ID] {
			remaining[m.ID] = m
		}
	}
	result := make([]Media, 0, 5)
	rot.upcoming = slices.DeleteFunc(rot.upcoming, func(id string) bool {
		m, ok := remaining[id]
		if ok {
			result = append(result, m)
			delete(remaining, id)
		}
		return !ok
	})
	if len(result) == 5 || len(remaining) == 0 {
		return result
	}
	simulated := *rot
	simulated.played = maps.Clone(rot.played)
	plannedHistory := make([]Entry, 0, len(history)+5)
	for i := len(result) - 1; i >= 0; i-- {
		m := result[i]
		simulated.played[m.ID] = true
		plannedHistory = append(plannedHistory, Entry{At: now, Track: m.Track})
	}
	plannedHistory = append(plannedHistory, history...)
	for len(result) < 5 && len(remaining) > 0 {
		m := simulated.pick(category, list, plannedHistory, rules, now)
		rot.upcoming = append(rot.upcoming, m.ID)
		result = append(result, m)
		delete(remaining, m.ID)
		plannedHistory = append([]Entry{{At: now, Track: m.Track}}, plannedHistory...)
	}
	return result
}

func (a *App) upcomingLocked(s Station) []Track {
	rot := a.rotate[s.Slug]
	out := []Track{}
	if rot == nil {
		return out
	}
	category := s.rotationCategory()
	now := time.Now()
	var eligible []Media
	for _, m := range a.state.Media {
		if m.Category == category && a.available(m, s.Slug, now) && (category != "jingles" || jingleTime(m.Tags, now.In(a.loc))) {
			eligible = append(eligible, m)
		}
	}
	if len(eligible) == 0 {
		return out
	}
	for _, m := range rot.plan(category, eligible, a.state.History[s.Slug], s.Rules, now) {
		out = append(out, m.Track)
	}
	return out
}

func (rot *rotation) pick(category string, list []Media, history []Entry, rules Rules, now time.Time) Media {
	rot.prepare(category, list)

	lastTrack, lastArtist := make(map[string]time.Time), make(map[string]time.Time)
	previous := ""
	for _, h := range history {
		if previous == "" && h.Category == category {
			previous = h.ID
		}
		if _, seen := lastTrack[h.ID]; !seen {
			lastTrack[h.ID] = h.At
		}
		if artist := strings.ToLower(h.Artist); artist != "" {
			if _, seen := lastArtist[artist]; !seen {
				lastArtist[artist] = h.At
			}
		}
	}

	best := -2.0
	var chosen Media
	for _, m := range list {
		if rot.played[m.ID] {
			continue
		}
		rank, exists := rot.rank[m.ID]
		if !exists {
			rank = randv2.Uint64()
			rot.rank[m.ID] = rank
		}
		score := 1.0
		if at, ok := lastTrack[m.ID]; ok && rules.RepeatSong > 0 {
			score = min(score, now.Sub(at).Minutes()/float64(rules.RepeatSong))
		}
		if at, ok := lastArtist[strings.ToLower(m.Artist)]; ok && rules.RepeatArtist > 0 {
			score = min(score, now.Sub(at).Minutes()/float64(rules.RepeatArtist))
		}
		score = max(0, score)
		if m.ID == previous {
			score = -1
		}
		if score > best || score == best && rank < rot.rank[chosen.ID] {
			chosen, best = m, score
		}
	}
	rot.played[chosen.ID] = true
	return chosen
}
func (a *App) stop() {
	a.imports.stop()
	a.mu.Lock()
	engines := a.engines
	a.engines = map[string]*Engine{}
	a.mu.Unlock()
	for _, e := range engines {
		e.close()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.saveLocked()
	a.db.close()
}
func findMedia(s *State, id string) (Media, bool) {
	for _, m := range s.Media {
		if m.ID == id {
			return m, true
		}
	}
	return Media{}, false
}
func hasStation(s *State, slug string) bool {
	return slices.ContainsFunc(s.Stations, func(x Station) bool { return x.Slug == slug })
}
func bad(s string) error { return fmt.Errorf("%s", s) }
