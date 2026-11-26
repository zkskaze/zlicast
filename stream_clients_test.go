package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestListenAddress(t *testing.T) {
	t.Setenv("ZLICAST_PORT", "")
	t.Setenv("ZLICAST_ADDR", "")
	if got := listenAddress(); got != ":1907" {
		t.Fatalf("default listen address is %q, want :1907", got)
	}
	t.Setenv("ZLICAST_ADDR", "localhost:1907")
	if got := listenAddress(); got != "localhost:1907" {
		t.Fatalf("legacy listen address is %q, want localhost:1907", got)
	}
	t.Setenv("ZLICAST_PORT", "19207")
	if got := listenAddress(); got != ":19207" {
		t.Fatalf("configured listen address is %q, want :19207", got)
	}
}

func TestBrowserPCMAndM3UShareLiveAudio(t *testing.T) {
	a, err := newApp(t.TempDir(), "admin", "test", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer a.stop()
	a.startStation(a.state.Stations[0])
	server, client := memoryServer(a.routes())
	defer server.Close()

	const origin = "http://radio.local:19207"
	request, _ := http.NewRequest("GET", origin+"/listen/zlifm.m3u", nil)
	request.Header.Set("Origin", "http://external-player.example")
	playlist, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(playlist.Body)
	playlist.Body.Close()
	if err != nil || playlist.StatusCode != 200 || playlist.Header.Get("Content-Disposition") != "" {
		t.Fatalf("playlist: %s, %s, %v", playlist.Status, playlist.Header, err)
	}
	if playlist.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("cross-origin player cannot fetch the playlist")
	}
	download, err := client.Get(origin + "/listen/zlifm.m3u?download=1")
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(download.Body)
	download.Body.Close()
	if err != nil || download.StatusCode != 200 || !bytes.Equal(data, downloaded) ||
		download.Header.Get("Content-Disposition") != `attachment; filename="zlifm.m3u"` {
		t.Fatalf("playlist download: %s, %s, %v", download.Status, download.Header, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\r\n")
	if len(lines) != 3 || lines[0] != "#EXTM3U" || lines[1] != "#EXTINF:-1,ZliFM" ||
		lines[2] != origin+"/stream/zlifm.mp3" {
		t.Fatalf("unexpected playlist: %q", data)
	}
	audio, err := client.Get(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Body.Close()
	if audio.StatusCode != 200 || audio.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatal("playlist does not lead to MP3", audio.Status, audio.Header)
	}
	pcm, err := client.Get(origin + "/stream/zlifm/pcm")
	if err != nil {
		t.Fatal(err)
	}
	defer pcm.Body.Close()
	if pcm.StatusCode != 200 || pcm.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatal("browser PCM is unavailable", pcm.Status, pcm.Header)
	}
	raw, mp3 := make([]byte, blockBytes*16), make([]byte, 8192)
	if _, err := io.ReadFull(pcm.Body, raw); err != nil {
		t.Fatal(err)
	}
	peak := 0
	for i := 0; i < len(raw); i += 2 {
		value := int(int16(binary.LittleEndian.Uint16(raw[i:])))
		peak = max(peak, value, -value)
	}
	if peak < 1500 {
		t.Fatal("browser received silence", peak)
	}
	if _, err := io.ReadFull(audio.Body, mp3); err != nil {
		t.Fatal(err)
	}
	decode := exec.Command("ffmpeg", "-v", "error", "-i", "pipe:0", "-f", "s16le", "pipe:1")
	decode.Stdin = bytes.NewReader(mp3)
	if decoded, err := decode.Output(); err != nil || len(decoded) < sampleRate {
		t.Fatal("playlist audio was not decodable", err, len(decoded))
	}
	if listeners := a.engine("zlifm").snapshot().Listeners; listeners != 2 {
		t.Fatal("expected two concurrent listeners", listeners)
	}
}
