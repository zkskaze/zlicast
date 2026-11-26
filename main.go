package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

//go:embed all:web/build
var assets embed.FS

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}
func result(w http.ResponseWriter, e error) {
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(v)
}
func loadEnv(dir string) error {
	path := filepath.Join(dir, ".env")
	if _, err := os.Stat(path); os.IsNotExist(err) && os.Getenv("ADMIN_PASSWORD") == "" {
		contents := fmt.Sprintf("ADMIN_USER=admin\nADMIN_PASSWORD=%s\nSOURCE_PASSWORD=%s\nZLICAST_PORT=1907\nZLICAST_DATA=data\nTZ=Europe/Belgrade\n", randomID()[:18], randomID())
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(contents)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(s.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == 39) && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if ok {
			if _, set := os.LookupEnv(k); !set {
				os.Setenv(k, v)
			}
		}
	}
	return s.Err()
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func listenAddress() string {
	if port := os.Getenv("ZLICAST_PORT"); port != "" {
		return net.JoinHostPort("", port)
	}
	return env("ZLICAST_ADDR", ":1907")
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	a.coreRoutes(mux)
	a.mediaRoutes(mux)
	a.automationRoutes(mux)
	a.studioRoutes(mux)
	a.ttsRoutes(mux)
	a.adminRoutes(mux)
	root, _ := fs.Sub(assets, "web/build")
	static := http.FileServer(http.FS(root))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if st, err := fs.Stat(root, p); err == nil && !st.IsDir() {
				static.ServeHTTP(w, r)
				return
			}
		}
		if p != "" && !strings.HasPrefix(p, "listen/") {
			http.NotFound(w, r)
			return
		}
		b, _ := fs.ReadFile(root, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicRead := strings.HasPrefix(r.URL.Path, "/stream/") || strings.HasPrefix(r.URL.Path, "/hls/") ||
			strings.HasPrefix(r.URL.Path, "/listen/") && strings.HasSuffix(r.URL.Path, ".m3u") ||
			strings.HasPrefix(r.URL.Path, "/api/stations/") && (strings.HasSuffix(r.URL.Path, "/now-playing") || strings.HasSuffix(r.URL.Path, "/history") || strings.HasSuffix(r.URL.Path, "/stats"))
		if publicRead && (r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Expose-Headers", "icy-name, icy-br, icy-metaint")
			if r.Method == "OPTIONS" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Icy-MetaData, Range")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != "" && !sameOrigin(r) {
			fail(w, 403, "Недозвољено порекло захтева.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func run() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if err = loadEnv(dir); err != nil {
		return err
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err = exec.LookPath(tool); err != nil {
			return fmt.Errorf("потребан је %s у PATH-у", tool)
		}
	}
	user, pw := env("ADMIN_USER", "admin"), os.Getenv("ADMIN_PASSWORD")
	if pw == "" {
		return errors.New("попуни ADMIN_PASSWORD у .env")
	}
	data := env("ZLICAST_DATA", "data")
	if !filepath.IsAbs(data) {
		data = filepath.Join(dir, data)
	}
	loc, err := time.LoadLocation(env("TZ", "Europe/Belgrade"))
	if err != nil {
		return err
	}
	addr := listenAddress()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer l.Close()
	a, err := newApp(data, user, pw, os.Getenv("SOURCE_PASSWORD"), loc)
	if err != nil {
		return err
	}
	defer a.stop()
	if err = a.restorePending(); err != nil {
		return err
	}
	for _, st := range a.state.Stations {
		a.startStation(st)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	clockDone := make(chan struct{})
	go func() { defer close(clockDone); a.clock(ctx) }()
	server := &http.Server{Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() { <-ctx.Done(); server.Close() }()
	bound := l.Addr().(*net.TCPAddr)
	host, _, _ := net.SplitHostPort(addr)
	if bound.IP.IsUnspecified() {
		host = "localhost"
	}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(bound.Port))
	fmt.Printf("Слуша на %s\n", bound)
	fmt.Printf("ZliCast · %s\nАдминистратор: %s  |  приступни подаци: %s\n", url, user, filepath.Join(dir, ".env"))
	if runtime.GOOS == "windows" && env("ZLICAST_OPEN_BROWSER", "1") == "1" && (bound.IP.IsUnspecified() || bound.IP.IsLoopback()) {
		cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		if cmd.Start() == nil {
			go cmd.Wait()
		}
	}
	err = server.Serve(l)
	cancel()
	<-clockDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
