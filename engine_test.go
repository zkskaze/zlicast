package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestBroadcastSharesAudibleFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"mp3", "aac"} {
		t.Run(format, func(t *testing.T) {
			e := newEngine(ProgramConfig{Name: "ZliFM", Format: format, Bitrate: 128}, func() Track { return Track{} }, func(Track) {})
			s, client := memoryServer(http.HandlerFunc(e.stream))
			defer s.Close()
			defer e.close()

			one, err := client.Get("http://radio.local/stream")
			if err != nil {
				t.Fatal(err)
			}
			defer one.Body.Close()
			if _, err := io.ReadFull(one.Body, make([]byte, 4096)); err != nil {
				t.Fatal(err)
			}
			two, err := client.Get("http://radio.local/stream")
			if err != nil {
				t.Fatal(err)
			}
			defer two.Body.Close()
			a, b := make([]byte, 24000), make([]byte, 12000)
			if _, err = io.ReadFull(two.Body, b); err != nil {
				t.Fatal(err)
			}
			if _, err = io.ReadFull(one.Body, a); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(a, b[:8000]) {
				t.Fatal("listeners received different frames")
			}
			cmd := exec.Command("ffmpeg", "-v", "error", "-i", "pipe:0", "-f", "s16le", "-ac", "1", "pipe:1")
			cmd.Stdin = bytes.NewReader(b)
			pcm, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			peak := 0
			for i := 0; i+1 < len(pcm); i += 2 {
				peak = max(peak, int(int16(binary.LittleEndian.Uint16(pcm[i:]))))
			}
			if peak < 1500 {
				t.Fatalf("unexpectedly quiet: %v", peak)
			}
			t.Logf("two matching listeners, decoded %d PCM samples, peak %d/32767", len(pcm)/2, peak)
			if e.snapshot().Listeners != 2 {
				t.Fatal(e.snapshot())
			}
		})
	}
}

type pipeListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }
func memoryServer(h http.Handler) (*http.Server, *http.Client) {
	l := &pipeListener{ch: make(chan net.Conn), done: make(chan struct{})}
	s := &http.Server{Handler: h}
	go s.Serve(l)
	tr := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		a, b := net.Pipe()
		select {
		case l.ch <- a:
			return b, nil
		case <-ctx.Done():
			a.Close()
			b.Close()
			return nil, ctx.Err()
		}
	}}
	return s, &http.Client{Transport: tr, Timeout: 20 * time.Second}
}
