package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

var categories = []string{"music", "jingles", "news", "ads", "shows", "emergency"}

const uploadLimit int64 = 512 << 20

var audioInputFlags = []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,flac,wav,ogg,mov,matroska,webm,aac,asf,aiff"}

func normalizeAudio(ctx context.Context, raw, wav string) error {
	args := append(slices.Clone(audioInputFlags), "-nostdin", "-y", "-i", raw, "-map", "0:a:0", "-vn", "-t", "14400", "-af", "loudnorm=I=-16:TP=-1.5:LRA=11", "-ar", "48000", "-ac", "2", "-c:a", "pcm_s16le", "-map_metadata", "-1", wav)
	if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
		return bad("Обрада звука није успела. Провери фајл и слободан простор.")
	}
	return nil
}

type probeInfo struct {
	Format struct {
		Duration string
		Tags     map[string]string
	}
	Streams []struct {
		Codec_type  string
		Disposition struct{ Attached_pic int }
	}
}

func (a *App) stageMedia(src io.Reader) (work, digest string, err error) {
	work, err = os.MkdirTemp(a.data, ".import-")
	if err != nil {
		return "", "", err
	}
	staging := work
	defer func() {
		if err != nil {
			os.RemoveAll(staging)
		}
	}()
	f, err := os.Create(filepath.Join(work, "input"))
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(src, uploadLimit+1))
	closeErr := f.Close()
	if err != nil {
		return "", "", err
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if n > uploadLimit || n == 0 {
		return "", "", bad("Фајл мора бити између 1 бајта и 512 MiB.")
	}
	return work, hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *App) addMedia(ctx context.Context, user string, src io.Reader, filename, cat, slug, tags, expiry string) (Media, bool, error) {
	work, digest, err := a.stageMedia(src)
	if err != nil {
		return Media{}, false, err
	}
	defer os.RemoveAll(work)
	return a.importStagedMedia(ctx, user, work, digest, filename, cat, slug, tags, expiry)
}

func (a *App) importStagedMedia(ctx context.Context, user, work, digest, filename, cat, slug, tags, expiry string) (Media, bool, error) {
	var m Media
	if !slices.Contains(categories, cat) {
		return m, false, bad("Непозната категорија.")
	}
	if expiry != "" {
		if _, err := time.Parse("2006-01-02", expiry); err != nil {
			return m, false, bad("Провери датум истека.")
		}
	}
	raw := filepath.Join(work, "input")
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	if err := ctx.Err(); err != nil {
		return m, false, err
	}
	a.mu.RLock()
	stationOK := slug == "" || hasStation(&a.state, slug)
	for _, v := range a.state.Media {
		if v.Hash == digest && v.Station == slug && v.Category == cat {
			a.mu.RUnlock()
			return v, true, nil
		}
	}
	a.mu.RUnlock()
	if !stationOK {
		return m, false, bad("Непозната станица.")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	opts := audioInputFlags
	args := append(slices.Clone(opts), "-show_entries", "format=duration:format_tags=title,artist:stream=codec_type:stream_disposition=attached_pic", "-of", "json", raw)
	out, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		return m, false, bad("FFprobe није препознао аудио-фајл.")
	}
	var p probeInfo
	if json.Unmarshal(out, &p) != nil {
		return m, false, bad("Неисправни метаподаци.")
	}
	hasAudio := false
	hasCover := false
	for _, st := range p.Streams {
		hasAudio = hasAudio || st.Codec_type == "audio"
		hasCover = hasCover || st.Disposition.Attached_pic == 1
	}
	if !hasAudio {
		return m, false, bad("Овај фајл нема аудио запис.")
	}
	duration, _ := strconv.ParseFloat(p.Format.Duration, 64)
	if duration > 4*3600 {
		return m, false, bad("Један снимак може да траје до четири сата.")
	}
	m.ID = randomID()
	m.Hash = digest
	m.Title = strings.TrimSuffix(filepath.Base(strings.ReplaceAll(filename, "\\", "/")), filepath.Ext(filename))
	m.Category = cat
	m.Station = slug
	m.Expires = expiry
	m.Tags = []string{}
	for k, v := range p.Format.Tags {
		switch strings.ToLower(k) {
		case "artist":
			m.Artist = v
		case "title":
			if strings.TrimSpace(v) != "" {
				m.Title = v
			}
		}
	}
	for _, t := range strings.Split(tags, ",") {
		if v := strings.TrimSpace(t); v != "" && !slices.Contains(m.Tags, v) {
			m.Tags = append(m.Tags, v)
		}
	}
	path := a.mediaPath(m)
	if _, err = os.Stat(path); errors.Is(err, os.ErrNotExist) {
		wav := filepath.Join(work, "normalized.wav")
		if err = normalizeAudio(ctx, raw, wav); err != nil {
			return m, false, err
		}
		if err = os.Rename(wav, path); err != nil {
			return m, false, err
		}
	} else if err != nil {
		return m, false, err
	}
	m.Path = path
	deck, err := openDeck(m.Track)
	if err != nil {
		return m, false, err
	}
	m.Duration = float64(deck.total) / 192000
	deck.close()
	coverPath := filepath.Join(a.data, "media", digest+".jpg")
	if hasCover {
		if _, err = os.Stat(coverPath); os.IsNotExist(err) {
			cover := filepath.Join(work, "cover.jpg")
			args = append(slices.Clone(opts), "-nostdin", "-i", raw, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=480:480:force_original_aspect_ratio=decrease", cover)
			if exec.CommandContext(ctx, "ffmpeg", args...).Run() == nil {
				_ = os.Rename(cover, coverPath)
			}
		}
	}
	if _, err = os.Stat(coverPath); err == nil {
		m.Cover = "/api/media/" + m.ID + "/cover"
	}
	err = a.change(user, "Додат снимак · "+m.Title, func(s *State) error {
		if slug != "" && !hasStation(s, slug) {
			return bad("Станица је уклоњена.")
		}
		s.Media = append(s.Media, m)
		return nil
	})
	if err == nil {
		a.mu.RLock()
		for _, s := range a.state.Stations {
			cat := s.Rules.Category
			if cat == "" {
				cat = "music"
			}
			e := a.engines[s.Slug]
			if e != nil && m.Category == cat && a.available(m, s.Slug, time.Now()) && e.snapshot().Now.Category == "fallback" {
				e.queue([]Track{a.track(m)}, true)
			}
		}
		a.mu.RUnlock()
	}
	return m, false, err
}

func (a *App) playBulletin(w http.ResponseWriter, r *http.Request, user string) {
	e := a.engine(r.PathValue("slug"))
	if e == nil {
		fail(w, 404, "Станица није пронађена.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		fail(w, 400, "Неисправна или превелика датотека.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "Изабери фајл.")
		return
	}
	defer file.Close()
	work, err := os.MkdirTemp("", "zlicast-bulletin-input-")
	if err != nil {
		fail(w, 500, "Није могуће припремити снимак.")
		return
	}
	defer os.RemoveAll(work)
	raw := filepath.Join(work, "input")
	target, err := os.Create(raw)
	if err != nil {
		fail(w, 500, "Није могуће припремити снимак.")
		return
	}
	n, err := io.Copy(target, io.LimitReader(file, uploadLimit+1))
	closeErr := target.Close()
	if err != nil || closeErr != nil || n == 0 || n > uploadLimit {
		fail(w, 400, "Неисправна или превелика датотека.")
		return
	}
	wav, err := os.CreateTemp("", "zlicast-bulletin-*.wav")
	if err != nil {
		fail(w, 500, "Није могуће припремити снимак.")
		return
	}
	wav.Close()
	temporary := Track{ID: randomID(), Title: strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename)), Category: "news", Path: wav.Name(), Temporary: true}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	if err = normalizeAudio(ctx, raw, wav.Name()); err != nil {
		temporary.discard()
		fail(w, 400, err.Error())
		return
	}
	d, err := openDeck(Track{Path: wav.Name()})
	if err != nil {
		temporary.discard()
		fail(w, 400, "Снимак нема звук за емитовање.")
		return
	}
	temporary.Duration = float64(d.total) / 192000
	d.close()
	if !e.queue([]Track{temporary}, true) {
		temporary.discard()
		fail(w, 409, "Станица тренутно не прима снимке.")
		return
	}
	respond(w, 200, true)
}

func (a *App) mediaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/stations/{slug}/bulletin", a.permit("admin,editor,dj", a.playBulletin))
	mux.HandleFunc("POST /api/media", a.permit("admin,editor,dj", func(w http.ResponseWriter, r *http.Request, user string) {
		r.Body = http.MaxBytesReader(w, r.Body, uploadLimit+(1<<20))
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			fail(w, 400, "Неисправна или превелика датотека.")
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("file")
		if err != nil {
			fail(w, 400, "Изабери фајл.")
			return
		}
		defer file.Close()
		work, digest, err := a.stageMedia(file)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		category, station, tags, expires := r.FormValue("category"), r.FormValue("station"), r.FormValue("tags"), r.FormValue("expires")
		if r.URL.Query().Get("async") == "1" {
			id, ok := a.imports.start(user, func(ctx context.Context) (Media, bool, error) {
				defer os.RemoveAll(work)
				return a.importStagedMedia(ctx, user, work, digest, header.Filename, category, station, tags, expires)
			})
			if !ok {
				os.RemoveAll(work)
				fail(w, 503, "Ред за обраду је пун. Покушај поново кад се ослободи.")
				return
			}
			respond(w, 202, map[string]string{"job": id})
			return
		}
		defer os.RemoveAll(work)
		m, dup, err := a.importStagedMedia(r.Context(), user, work, digest, header.Filename, category, station, tags, expires)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]any{"media": m, "duplicate": dup})
	}))
	mux.HandleFunc("GET /api/media/imports/{job}", a.permit("admin,editor,dj", a.imports.status))
	mux.HandleFunc("GET /api/media/{id}/{kind}", a.permit("*", func(w http.ResponseWriter, r *http.Request, user string) {
		a.mu.RLock()
		m, ok := findMedia(&a.state, r.PathValue("id"))
		a.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		path := a.mediaPath(m)
		switch r.PathValue("kind") {
		case "audio":
			w.Header().Set("Content-Type", "audio/wav")
		case "cover":
			path = filepath.Join(a.data, "media", m.Hash+".jpg")
			w.Header().Set("Content-Type", "image/jpeg")
		default:
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	}))
	mux.HandleFunc("DELETE /api/media/{id}", a.permit("admin,editor", func(w http.ResponseWriter, r *http.Request, user string) {
		_, err := a.deleteMedia(user, []string{r.PathValue("id")})
		result(w, err)
	}))
	mux.HandleFunc("POST /api/media/bulk-delete", a.permit("admin,editor", func(w http.ResponseWriter, r *http.Request, user string) {
		var form struct{ IDs []string }
		if err := decode(r, &form); err != nil || len(form.IDs) == 0 || len(form.IDs) > 5000 {
			fail(w, 400, "Изабери од 1 до 5000 снимака.")
			return
		}
		n, err := a.deleteMedia(user, form.IDs)
		if err != nil {
			result(w, err)
			return
		}
		respond(w, 200, map[string]int{"deleted": n})
	}))
}

func (a *App) deleteMedia(user string, ids []string) (int, error) {
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	discarded := make(map[string]Media)
	count := 0
	err := a.change(user, "Уклоњени снимци из медијатеке", func(s *State) error {
		s.Media = slices.DeleteFunc(s.Media, func(m Media) bool {
			if !selected[m.ID] {
				return false
			}
			discarded[m.Hash] = m
			count++
			return true
		})
		if count == 0 {
			return bad("Изабрани снимци нису пронађени.")
		}
		for _, m := range s.Media {
			delete(discarded, m.Hash)
		}
		for i := range s.Playlists {
			s.Playlists[i].Tracks = slices.DeleteFunc(s.Playlists[i].Tracks, func(id string) bool { return selected[id] })
		}
		for i := range s.Stations {
			if selected[s.Stations[i].Rules.JingleID] {
				s.Stations[i].Rules.JingleID = ""
			}
		}
		s.Events = slices.DeleteFunc(s.Events, func(e Event) bool { return e.Target == "track" && selected[e.Value] })
		return nil
	})
	if err == nil {
		for _, m := range discarded {
			os.Remove(a.mediaPath(m))
			os.Remove(filepath.Join(a.data, "media", m.Hash+".jpg"))
		}
	}
	return count, err
}
