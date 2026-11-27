package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

const youtubeCookiesFile = "youtube-cookies.txt"

func youtubeVideoID(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	path := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	var id string
	switch strings.ToLower(u.Host) {
	case "youtu.be", "www.youtu.be":
		id = path
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if path == "watch" {
			id = u.Query().Get("v")
		} else {
			kind, value, _ := strings.Cut(path, "/")
			switch kind {
			case "shorts", "live", "embed":
				id = value
			}
		}
	}
	if !youtubeID.MatchString(id) {
		return ""
	}
	return id
}

func (a *App) submitYouTubeImport(w http.ResponseWriter, r *http.Request, user string) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var form struct {
		URL string `json:"url"`
		mediaImportOptions
	}
	if err := decode(r, &form); err != nil {
		fail(w, 400, "Неисправан захтев за увоз са YouTube-а.")
		return
	}
	id := youtubeVideoID(form.URL)
	if id == "" {
		fail(w, 400, "Налепи YouTube линк до једног снимка.")
		return
	}
	if form.Category == "" {
		form.Category = "music"
	}
	form.Title = strings.TrimSpace(form.Title)
	if err := form.mediaImportOptions.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.mu.RLock()
	stationOK := form.Station == "" || hasStation(&a.state, form.Station)
	a.mu.RUnlock()
	if !stationOK {
		fail(w, 400, "Непозната станица.")
		return
	}
	downloader, err := exec.LookPath(env("ZLICAST_YT_DLP", "yt-dlp"))
	if err == nil {
		downloader, err = filepath.Abs(downloader)
	}
	if err != nil {
		fail(w, 503, "Инсталирај yt-dlp и Deno на серверу. Додај их у PATH или упиши путању до yt-dlp у ZLICAST_YT_DLP.")
		return
	}
	options := form.mediaImportOptions
	job, ok := a.imports.start(user, func(ctx context.Context) (Media, bool, error) {
		return a.importYouTube(ctx, user, downloader, id, options)
	})
	if !ok {
		fail(w, 503, "Ред за обраду је пун. Покушај поново кад се ослободи.")
		return
	}
	respond(w, 202, map[string]string{"job": job})
}

func (a *App) importYouTube(ctx context.Context, user, downloader, id string, options mediaImportOptions) (Media, bool, error) {
	metadataDir, err := os.MkdirTemp(a.data, ".youtube-")
	if err != nil {
		return Media{}, false, err
	}
	defer os.RemoveAll(metadataDir)

	hasCookies, err := stageYouTubeCookies(a.data, metadataDir)
	if err != nil {
		return Media{}, false, err
	}

	downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	const metadataFile = "labels.json"
	args := []string{
		"--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--no-update",
		"--no-playlist", "--no-progress", "--quiet", "--no-simulate", "--socket-timeout", "12", "--retries", "1",
		"--format", "bestaudio[protocol=https]", "--match-filters", "duration <= 14400 & !is_live",
		"--max-filesize", "512M", "--output", "-", "--output-na-placeholder", "null",
		"--print-to-file", `before_dl:{"title":%(track,title).500j,"artist":%(artist,uploader).500j}`, metadataFile,
	}
	if hasCookies {
		args = append(args, "--cookies", youtubeCookiesFile)
	}
	args = append(args, "--", "https://www.youtube.com/watch?v="+id)
	cmd := exec.CommandContext(downloadCtx, downloader, args...)
	cmd.Dir = metadataDir
	var diagnostic youtubeDiagnostic
	cmd.Stderr = io.MultiWriter(os.Stderr, &diagnostic)
	audio, err := cmd.StdoutPipe()
	if err != nil {
		return Media{}, false, bad("Није могуће покренути yt-dlp.")
	}
	if err := cmd.Start(); err != nil {
		return Media{}, false, bad("Није могуће покренути yt-dlp.")
	}
	stopClosing := context.AfterFunc(downloadCtx, func() { _ = audio.Close() })
	defer stopClosing()
	work, digest, stageErr := a.stageMedia(audio)
	if stageErr == nil {
		defer os.RemoveAll(work)
	} else if !errors.Is(stageErr, errEmptyMedia) {
		cancel()
		_ = audio.Close()
	}
	downloadErr := cmd.Wait()
	if errors.Is(downloadCtx.Err(), context.DeadlineExceeded) {
		return Media{}, false, bad("Преузимање са YouTube-а трајало је дуже од пет минута.")
	}
	if stageErr != nil && !errors.Is(stageErr, errEmptyMedia) {
		return Media{}, false, stageErr
	}
	if downloadErr != nil || stageErr != nil {
		return Media{}, false, youtubeDownloadError(diagnostic.tail, hasCookies)
	}

	var labels mediaLabels
	if raw, err := os.ReadFile(filepath.Join(metadataDir, metadataFile)); err == nil {
		_ = json.Unmarshal(raw, &labels)
	}
	if strings.TrimSpace(labels.Title) == "" {
		labels.Title = "YouTube " + id
	}
	return a.importStagedMedia(ctx, user, work, digest, labels, options)
}

func stageYouTubeCookies(dataDir, workDir string) (bool, error) {
	content, err := os.ReadFile(filepath.Join(dataDir, youtubeCookiesFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, bad("Сервер не може да прочита фајл youtube-cookies.txt.")
	}
	header, _, _ := bytes.Cut(content, []byte("\n"))
	switch string(bytes.TrimSpace(header)) {
	case "# Netscape HTTP Cookie File", "# HTTP Cookie File":
	default:
		return false, bad("Извези YouTube колачиће у Netscape формату и сачувај их као youtube-cookies.txt.")
	}
	// yt-dlp writes to its cookie jar. Each import gets a separate copy.
	if err := os.WriteFile(filepath.Join(workDir, youtubeCookiesFile), content, 0600); err != nil {
		return false, bad("Сервер не може да припреми YouTube колачиће.")
	}
	return true, nil
}

type youtubeDiagnostic struct {
	tail string
}

func (d *youtubeDiagnostic) Write(p []byte) (int, error) {
	const maxDiagnostic = 8192
	d.tail += string(p)
	if len(d.tail) > maxDiagnostic {
		d.tail = d.tail[len(d.tail)-maxDiagnostic:]
	}
	return len(p), nil
}

func youtubeDownloadError(details string, hasCookies bool) error {
	message := strings.ToLower(strings.ReplaceAll(details, "’", "'"))
	if strings.Contains(message, "sign in to confirm you're not a bot") {
		if hasCookies {
			return bad("YouTube и даље тражи пријаву са овог сервера. Освежи YouTube колачиће у фајлу youtube-cookies.txt и покушај поново.")
		}
		return bad("YouTube тражи пријаву са овог сервера. Постави YouTube колачиће у data/youtube-cookies.txt.")
	}
	if hasCookies && strings.Contains(message, "account cookies are no longer valid") {
		return bad("YouTube колачићи више не важе. Извези нове у youtube-cookies.txt и покушај поново.")
	}
	return bad("Преузимање са YouTube-а није успело. Детаљи су у журналу сервера. Увоз подржава завршене снимке до четири сата и 512 MiB.")
}
