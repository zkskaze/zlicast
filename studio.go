package main

import (
	"golang.org/x/net/websocket"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

func pushPCM(e *Engine, f *liveFeed, b []byte) bool {
	select {
	case f.data <- b:
		return true
	case <-e.ctx.Done():
		return false
	case <-f.stop:
		return false
	case <-time.After(2 * time.Second):
		return false
	}
}
func (a *App) studioRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/stations/{slug}/live", a.permit("admin,dj", func(w http.ResponseWriter, r *http.Request, user string) {
		if !sameOrigin(r) {
			fail(w, 403, "Порекло студија није дозвољено.")
			return
		}
		e := a.engine(r.PathValue("slug"))
		if e == nil {
			http.NotFound(w, r)
			return
		}
		title := strings.TrimSpace(r.URL.Query().Get("title"))
		if title == "" {
			title = "Студио уживо"
		}
		if len(title) > 256 {
			fail(w, 400, "Назив емисије је предугачак.")
			return
		}
		f, ok := e.beginLive(title)
		if !ok {
			fail(w, 409, "Студио је већ заузет.")
			return
		}
		defer e.endLive(f)
		websocket.Handler(func(ws *websocket.Conn) {
			defer ws.Close()
			ws.MaxPayloadBytes = blockBytes * 16
			var remain []byte
			for {
				ws.SetReadDeadline(time.Now().Add(6 * time.Second))
				var packet []byte
				if err := websocket.Message.Receive(ws, &packet); err != nil {
					return
				}
				if len(packet)%4 != 0 {
					return
				}
				remain = append(remain, packet...)
				for len(remain) >= blockBytes {
					b := append([]byte(nil), remain[:blockBytes]...)
					remain = remain[blockBytes:]
					if !pushPCM(e, f, b) {
						return
					}
				}
			}
		}).ServeHTTP(w, r)
	}))
	m.HandleFunc("PUT /source/{slug}", func(w http.ResponseWriter, r *http.Request) {
		user, pw, ok := r.BasicAuth()
		if a.source == "" || !ok || !equal(user, "source") || !equal(pw, a.source) {
			w.Header().Set("WWW-Authenticate", `Basic realm="ZliCast source"`)
			fail(w, 401, "Провери лозинку извора.")
			return
		}
		e := a.engine(r.PathValue("slug"))
		if e == nil {
			http.NotFound(w, r)
			return
		}
		f, ok := e.beginLive("Спољни студио")
		if !ok {
			fail(w, 409, "Студио је заузет.")
			return
		}
		defer e.endLive(f)
		format := "mp3"
		switch strings.Split(r.Header.Get("Content-Type"), ";")[0] {
		case "audio/aac", "audio/aacp":
			format = "aac"
		case "audio/ogg", "application/ogg":
			format = "ogg"
		case "audio/mpeg", "audio/mp3", "":
		default:
			fail(w, 415, "Подржани су MP3, AAC и OGG.")
			return
		}
		ctl := http.NewResponseController(w)
		ctl.EnableFullDuplex()
		cmd := exec.CommandContext(r.Context(), "ffmpeg", "-nostdin", "-v", "error", "-f", format, "-i", "pipe:0", "-ar", "48000", "-ac", "2", "-f", "s16le", "pipe:1")
		cmd.Stdin = r.Body
		out, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			fail(w, 500, "Декодер није доступан.")
			return
		}
		defer func() { r.Body.Close(); cmd.Process.Kill(); cmd.Wait() }()
		w.WriteHeader(200)
		ctl.Flush()
		for {
			b := make([]byte, blockBytes)
			if _, err := io.ReadFull(out, b); err != nil {
				return
			}
			if !pushPCM(e, f, b) {
				return
			}
		}
	})
}
