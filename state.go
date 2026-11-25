package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	JingleMinutes int    `json:"jingleMinutes"`
	IDHourly      bool   `json:"idHourly"`
	AdMinutes     int    `json:"adMinutes"`
	RepeatSong    int    `json:"repeatSong"`
	RepeatArtist  int    `json:"repeatArtist"`
	Category      string `json:"category"`
}
type Station struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	Bitrate   int    `json:"bitrate"`
	Crossfade int    `json:"crossfade"`
	Logo      string `json:"logo"`
	Color     string `json:"color"`
	Rules     Rules  `json:"rules"`
}

func (s Station) config() ProgramConfig {
	return ProgramConfig{s.Name, s.Format, s.Bitrate, s.Crossfade}
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
	user    string
	expires time.Time
}
type rotation struct {
	songs      int
	jingle, ad time.Time
	hour       string
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
	if err = db.exec("PRAGMA busy_timeout=2000; PRAGMA locking_mode=EXCLUSIVE; CREATE TABLE IF NOT EXISTS config(id INTEGER PRIMARY KEY,body TEXT NOT NULL)"); err != nil {
		db.close()
		return nil, err
	}
	b, err := db.read()
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
	return a, nil
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
func (a *App) engine(slug string) *Engine { a.mu.RLock(); defer a.mu.RUnlock(); return a.engines[slug] }
func (a *App) startStation(s Station) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.engines[s.Slug]; e != nil {
		e.configure(s.config())
		return
	}
	a.rotate[s.Slug] = &rotation{jingle: time.Now(), ad: time.Now(), hour: time.Now().In(a.loc).Format("2006-01-02T15")}
	a.engines[s.Slug] = newEngine(s.config(), func() Track { return a.next(s.Slug) }, func(t Track) { a.onPlay(s.Slug, t) })
}
func (a *App) onPlay(slug string, t Track) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engines[slug] == nil {
		return
	}
	h := append([]Entry{{time.Now(), t}}, a.state.History[slug]...)
	a.state.History[slug] = h[:min(500, len(h))]
	if t.Category == "music" {
		a.rotate[slug].songs++
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
	category := r.Category
	if category == "" {
		category = "music"
	}
	hour := now.In(a.loc).Format("2006-01-02T15")
	jingle := r.IDHourly && rot.hour != hour || r.JingleSongs > 0 && rot.songs >= r.JingleSongs || r.JingleMinutes > 0 && now.Sub(rot.jingle) >= time.Duration(r.JingleMinutes)*time.Minute
	ad := r.AdMinutes > 0 && now.Sub(rot.ad) >= time.Duration(r.AdMinutes)*time.Minute
	candidates := func(cat string) []Media {
		var out []Media
		for _, m := range a.state.Media {
			if m.Category == cat && a.available(m, slug, now) {
				out = append(out, m)
			}
		}
		return out
	}
	if jingle {
		if list := candidates("jingles"); len(list) > 0 {
			rot.jingle = now
			rot.songs = 0
			rot.hour = hour
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
	// Prefer an eligible track. With a tiny library use the least recently played.
	best := -1.0
	var chosen Media
	randv2.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	for _, m := range list {
		score := 1.0
		for _, h := range a.state.History[slug] {
			elapsed := now.Sub(h.At).Minutes()
			if r.RepeatSong > 0 && m.ID == h.ID {
				score = min(score, elapsed/float64(r.RepeatSong))
			}
			if r.RepeatArtist > 0 && m.Artist != "" && strings.EqualFold(m.Artist, h.Artist) {
				score = min(score, elapsed/float64(r.RepeatArtist))
			}
		}
		if score > best {
			best = score
			chosen = m
		}
		if score >= 1 {
			break
		}
	}
	return a.track(chosen)
}
func (a *App) stop() {
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
