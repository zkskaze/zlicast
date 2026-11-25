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

func (a *App) addMedia(ctx context.Context, user string, src io.Reader, filename, cat, slug, tags, expiry string) (Media, bool, error) {
	var m Media
	if !slices.Contains(categories, cat) {
		return m, false, bad("Непозната категорија.")
	}
	if expiry != "" {
		if _, err := time.Parse("2006-01-02", expiry); err != nil {
			return m, false, bad("Провери датум истека.")
		}
	}
	work, err := os.MkdirTemp(a.data, ".import-")
	if err != nil {
		return m, false, err
	}
	defer os.RemoveAll(work)
	raw := filepath.Join(work, "input")
	f, err := os.Create(raw)
	if err != nil {
		return m, false, err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(src, uploadLimit+1))
	closeErr := f.Close()
	if err != nil {
		return m, false, err
	}
	if closeErr != nil {
		return m, false, closeErr
	}
	if n > uploadLimit || n == 0 {
		return m, false, bad("Фајл мора бити између 1 бајта и 512 MiB.")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
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
	opts := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,flac,wav,ogg,mov,matroska,webm,aac,asf,aiff"}
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
		args = append(slices.Clone(opts), "-nostdin", "-i", raw, "-map", "0:a:0", "-vn", "-t", "14400", "-af", "loudnorm=I=-16:TP=-1.5:LRA=11", "-ar", "48000", "-ac", "2", "-c:a", "pcm_s16le", "-map_metadata", "-1", wav)
		if err = exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
			return m, false, bad("Обрада звука није успела. Провери фајл и слободан простор.")
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
func (a *App) mediaRoutes(mux *http.ServeMux) {
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
		m, dup, err := a.addMedia(r.Context(), user, file, header.Filename, r.FormValue("category"), r.FormValue("station"), r.FormValue("tags"), r.FormValue("expires"))
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]any{"media": m, "duplicate": dup})
	}))
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
		a.uploadMu.Lock()
		defer a.uploadMu.Unlock()
		var discarded Media
		id := r.PathValue("id")
		err := a.change(user, "Уклоњен снимак", func(s *State) error {
			m, ok := findMedia(s, id)
			if !ok {
				return bad("Непознат снимак.")
			}
			s.Media = slices.DeleteFunc(s.Media, func(m Media) bool { return m.ID == id })
			for i := range s.Playlists {
				s.Playlists[i].Tracks = slices.DeleteFunc(s.Playlists[i].Tracks, func(v string) bool { return v == id })
			}
			s.Events = slices.DeleteFunc(s.Events, func(e Event) bool { return e.Target == "track" && e.Value == id })
			if !slices.ContainsFunc(s.Media, func(v Media) bool { return v.Hash == m.Hash }) {
				discarded = m
			}
			return nil
		})
		if err == nil && discarded.ID != "" {
			os.Remove(a.mediaPath(discarded))
			os.Remove(filepath.Join(a.data, "media", discarded.Hash+".jpg"))
		}
		result(w, err)
	}))
}
