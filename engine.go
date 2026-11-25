package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const sampleRate = 48000
const blockFrames = 960
const blockBytes = blockFrames * 4

type Track struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Category string  `json:"category"`
	Duration float64 `json:"duration"`
	Path     string  `json:"-"`
}
type Status struct {
	Now            Track     `json:"now"`
	Started        time.Time `json:"started"`
	OnAir          bool      `json:"onAir"`
	Live           bool      `json:"live"`
	Level          float64   `json:"level"`
	Listeners      int       `json:"listeners"`
	Peak           int       `json:"peak"`
	Bytes          int64     `json:"bytes"`
	Errors         int64     `json:"errors"`
	AverageSeconds float64   `json:"averageSeconds"`
}
type ProgramConfig struct {
	Name, Format       string
	Bitrate, Crossfade int
}
type cue struct {
	tracks    []Track
	immediate bool
}
type liveFeed struct {
	title string
	data  chan []byte
	stop  chan struct{}
}
type Engine struct {
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	mu          sync.Mutex
	config      ProgramConfig
	status      Status
	subscribers map[chan []byte]time.Time
	totalListen time.Duration
	sessions    int
	cues        chan cue
	live        *liveFeed
	next        func() Track
	played      func(Track)
}

func newEngine(c ProgramConfig, next func() Track, played func(Track)) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{ctx: ctx, cancel: cancel, done: make(chan struct{}), config: c, next: next, played: played, cues: make(chan cue, 32), subscribers: map[chan []byte]time.Time{}}
	go e.run()
	return e
}
func (e *Engine) close() { e.cancel(); <-e.done }
func (e *Engine) snapshot() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.status
	s.Listeners = len(e.subscribers)
	d := e.totalListen
	for _, at := range e.subscribers {
		d += time.Since(at)
	}
	if n := e.sessions + s.Listeners; n > 0 {
		s.AverageSeconds = d.Seconds() / float64(n)
	}
	return s
}
func (e *Engine) queue(tracks []Track, immediate bool) bool {
	if len(tracks) == 0 {
		return false
	}
	select {
	case e.cues <- cue{tracks, immediate}:
		return true
	default:
		return false
	}
}
func (e *Engine) configure(c ProgramConfig) { e.mu.Lock(); e.config = c; e.mu.Unlock() }
func (e *Engine) getConfig() ProgramConfig  { e.mu.Lock(); defer e.mu.Unlock(); return e.config }
func (e *Engine) error()                    { e.mu.Lock(); e.status.Errors++; e.mu.Unlock() }

var fallbackOnce sync.Once
var theme []byte

func themeAudio() []byte {
	fallbackOnce.Do(func() {
		const beat = 0.48
		const beats = 32
		frames := int(float64(sampleRate) * beat * beats)
		theme = make([]byte, frames*4)
		roots := []float64{130.8128, 103.8262, 155.5635, 116.5409}
		notes := []float64{1, 1.5, 1.88775, 2.24492, 1.5, 1.1892, 2, 1.4983}
		for i := 0; i < frames; i++ {
			t := float64(i) / sampleRate
			b := int(t / beat)
			phase := math.Mod(t, beat)
			bar := b / 8
			root := roots[bar]
			l := math.Sin(2*math.Pi*root*notes[b%8]*phase) * math.Exp(-phase*7) * math.Min(phase*90, 1)
			bphase := math.Mod(t, 2*beat)
			bass := math.Sin(2*math.Pi*root/2*bphase) * math.Exp(-bphase*2.4) * math.Min(bphase*35, 1)
			kick := math.Sin(2*math.Pi*(47*phase+2.8*(1-math.Exp(-phase*30)))) * math.Exp(-phase*18)
			v := bass*.19 + kick*.12
			pan := .78 + .20*math.Sin(float64(b)*1.2)
			for ch, p := range []float64{pan, 1.56 - pan} {
				binary.LittleEndian.PutUint16(theme[i*4+ch*2:], uint16(int16((v+l*.17*p)*32767)))
			}
		}
	})
	return theme
}

type deck struct {
	track      Track
	file       *os.File
	pos, total int64
	demo       []byte
}

func fallback() *deck {
	b := themeAudio()
	return &deck{track: Track{ID: "@theme", Title: "Ноћни сигнал", Artist: "ZliCast", Category: "fallback", Duration: float64(len(b)) / 192000}, demo: b, total: int64(len(b))}
}
func openDeck(t Track) (*deck, error) {
	if t.Path == "" {
		return fallback(), nil
	}
	f, err := os.Open(t.Path)
	if err != nil {
		return nil, err
	}
	// Imported PCM is always stereo, 48 kHz, signed 16-bit little-endian WAV.
	var hdr [12]byte
	if _, err = io.ReadFull(f, hdr[:]); err == nil && string(hdr[:4]) == "RIFF" && string(hdr[8:]) == "WAVE" {
		var chunk [8]byte
		for {
			if _, err = io.ReadFull(f, chunk[:]); err != nil {
				break
			}
			n := int64(binary.LittleEndian.Uint32(chunk[4:]))
			if string(chunk[:4]) == "data" && n >= 4 {
				st, err := f.Stat()
				offset, _ := f.Seek(0, io.SeekCurrent)
				if err == nil && st.Size() >= offset+n {
					return &deck{track: t, file: f, total: n}, nil
				}
				break
			}
			if _, err = f.Seek(n+n%2, io.SeekCurrent); err != nil {
				break
			}
		}
	}
	f.Close()
	return nil, fmt.Errorf("неисправан WAV")
}
func (d *deck) read(b []byte) {
	clear(b)
	n := min(int64(len(b)), d.total-d.pos)
	if n <= 0 {
		return
	}
	if d.file != nil {
		got, _ := io.ReadFull(d.file, b[:n])
		if int64(got) < n {
			d.total = d.pos + int64(got)
			n = int64(got)
		}
	} else {
		copy(b, d.demo[d.pos:d.pos+n])
	}
	d.pos += n
}
func (d *deck) close() {
	if d != nil && d.file != nil {
		d.file.Close()
	}
}
func (e *Engine) setTrack(t Track, live bool) {
	e.mu.Lock()
	e.status.Now = t
	e.status.Started = time.Now()
	e.status.Live = live
	e.mu.Unlock()
	e.played(t)
}
func (e *Engine) run() {
	defer close(e.done)
	for e.ctx.Err() == nil {
		err := e.encode(e.getConfig())
		e.mu.Lock()
		e.status.OnAir = false
		if err != nil && e.ctx.Err() == nil {
			e.status.Errors++
		}
		e.mu.Unlock()
		if err != nil {
			select {
			case <-e.ctx.Done():
				return
			case <-time.After(350 * time.Millisecond):
			}
		}
	}
}
func (e *Engine) encode(cfg ProgramConfig) error {
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	codec, format := "libmp3lame", "mp3"
	if cfg.Format == "aac" {
		codec, format = "aac", "adts"
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-f", "s16le", "-ar", "48000", "-ac", "2", "-probesize", "32", "-analyzeduration", "0", "-i", "pipe:0", "-c:a", codec, "-b:a", fmt.Sprintf("%dk", cfg.Bitrate), "-flush_packets", "1"}
	if format == "mp3" {
		args = append(args, "-write_xing", "0", "-id3v2_version", "0")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", append(args, "-f", format, "pipe:1")...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		rd := bufio.NewReaderSize(stdout, 8192)
		for {
			b, err := encodedFrame(rd, format)
			if err != nil {
				return
			}
			e.mu.Lock()
			e.status.OnAir = true
			for sub, at := range e.subscribers {
				select {
				case sub <- b:
				default:
					close(sub)
					delete(e.subscribers, sub)
					e.totalListen += time.Since(at)
					e.sessions++
				}
			}
			e.mu.Unlock()
		}
	}()
	defer func() { stdin.Close(); cancel(); <-outDone; cmd.Wait() }()
	var current, previous *deck
	var pending []Track
	var wasLive bool
	var fadeLen, fadeAt, quiet int
	next := func(immediate bool) {
		t := Track{}
		if len(pending) > 0 {
			t = pending[0]
			pending = pending[1:]
		} else {
			t = e.next()
		}
		d, err := openDeck(t)
		if err != nil {
			e.error()
			d = fallback()
		}
		previous.close()
		previous = nil
		fadeLen = 0
		fadeAt = 0
		if current != nil {
			if !immediate && current.track.Category == "music" && d.track.Category == "music" {
				fadeLen = int(min(current.total-current.pos, d.total/2, int64(e.getConfig().Crossfade)*192000) / 4)
				if fadeLen > 0 {
					previous = current
				} else {
					current.close()
				}
			} else {
				current.close()
			}
		}
		current = d
		quiet = 0
		e.setTrack(d.track, false)
	}
	defer func() { current.close(); previous.close() }()
	b := make([]byte, blockBytes)
	old := make([]byte, blockBytes)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-outDone:
			return fmt.Errorf("енкодер је заустављен")
		case <-tick.C:
		}
		c := e.getConfig()
		if c.Format != cfg.Format || c.Bitrate != cfg.Bitrate {
			return nil
		}
		e.mu.Lock()
		live := e.live
		e.mu.Unlock()
		var liveBlock []byte
		if live != nil {
			select {
			case liveBlock = <-live.data:
			default:
			}
		}
		if liveBlock != nil || live != nil && wasLive {
			if !wasLive {
				e.setTrack(Track{ID: "@live", Title: live.title, Category: "live"}, true)
				wasLive = true
			}
			clear(b)
			copy(b, liveBlock)
		} else {
			if wasLive {
				wasLive = false
				next(true)
			}
			select {
			case q := <-e.cues:
				if q.immediate {
					pending = append(q.tracks, pending...)
					next(true)
				} else {
					pending = append(pending, q.tracks...)
				}
			default:
			}
			if current == nil || current.pos >= current.total {
				next(false)
			} else if current.track.Category == "music" && previous == nil && current.pos >= current.total/2 && current.total-current.pos <= int64(c.Crossfade)*192000 {
				next(false)
			}
			current.read(b)
			if previous != nil {
				previous.read(old)
				for i := 0; i < blockFrames; i++ {
					w := min(1, float64(fadeAt+i)/float64(fadeLen))
					for k := i * 4; k < i*4+4; k += 2 {
						v := float64(int16(binary.LittleEndian.Uint16(b[k:])))*w + float64(int16(binary.LittleEndian.Uint16(old[k:])))*(1-w)
						binary.LittleEndian.PutUint16(b[k:], uint16(int16(v)))
					}
				}
				fadeAt += blockFrames
				if fadeAt >= fadeLen {
					previous.close()
					previous = nil
				}
			}
		}
		peak := 0
		for i := 0; i < len(b); i += 8 {
			v := int(int16(binary.LittleEndian.Uint16(b[i:])))
			peak = max(peak, v, -v)
		}
		if peak < 8 && !wasLive {
			quiet++
			if quiet >= 250 {
				previous.close()
				previous = nil
				current.close()
				current = fallback()
				quiet = 0
				e.error()
				e.setTrack(current.track, false)
			}
		} else {
			quiet = 0
		}
		e.mu.Lock()
		e.status.Level = float64(peak) / 32768
		e.mu.Unlock()
		if _, err = stdin.Write(b); err != nil {
			return err
		}
	}
}
func encodedFrame(r *bufio.Reader, format string) ([]byte, error) {
	br := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	for {
		h, err := r.Peek(7)
		if err != nil {
			return nil, err
		}
		n := 0
		if format == "adts" && h[0] == 255 && h[1]&0xf6 == 0xf0 {
			n = int(h[3]&3)<<11 | int(h[4])<<3 | int(h[5])>>5
		}
		if format == "mp3" && h[0] == 255 && h[1]&0xfe == 0xfa && int(h[2]>>4) < len(br) && h[2]&12 == 4 {
			n = 144*br[h[2]>>4]*1000/sampleRate + int(h[2]>>1&1)
		}
		if n >= 7 && n <= 8192 {
			b := make([]byte, n)
			_, err = io.ReadFull(r, b)
			return b, err
		}
		r.Discard(1)
	}
}
func (e *Engine) stream(w http.ResponseWriter, r *http.Request) {
	sub := make(chan []byte, 96)
	e.mu.Lock()
	c := e.config
	e.subscribers[sub] = time.Now()
	e.status.Peak = max(e.status.Peak, len(e.subscribers))
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if at, ok := e.subscribers[sub]; ok {
			e.totalListen += time.Since(at)
			e.sessions++
			delete(e.subscribers, sub)
		}
		e.mu.Unlock()
	}()
	typ := "audio/mpeg"
	if c.Format == "aac" {
		typ = "audio/aac"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("icy-name", c.Name)
	w.Header().Set("icy-br", fmt.Sprint(c.Bitrate))
	icy := r.Header.Get("Icy-MetaData") == "1"
	const interval = 16384
	if icy {
		w.Header().Set("icy-metaint", "16384")
	}
	ctl := http.NewResponseController(w)
	ctl.Flush()
	left := interval
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-r.Context().Done():
			return
		case b, ok := <-sub:
			if !ok {
				return
			}
			ctl.SetWriteDeadline(time.Now().Add(5 * time.Second))
			size := len(b)
			for len(b) > 0 {
				n := len(b)
				if icy {
					n = min(n, left)
				}
				if _, err := w.Write(b[:n]); err != nil {
					return
				}
				b = b[n:]
				if icy {
					left -= n
					if left == 0 {
						t := e.snapshot().Now
						s := []byte("StreamTitle='" + strings.ReplaceAll(strings.Trim(t.Artist+" - "+t.Title, " -"), "'", "’") + "';")
						if len(s) > 4080 {
							s = s[:4080]
						}
						blocks := (len(s) + 15) / 16
						meta := make([]byte, 1+blocks*16)
						meta[0] = byte(blocks)
						copy(meta[1:], s)
						if _, err := w.Write(meta); err != nil {
							return
						}
						left = interval
					}
				}
			}
			if ctl.Flush() != nil {
				return
			}
			e.mu.Lock()
			e.status.Bytes += int64(size)
			e.mu.Unlock()
		}
	}
}
func (e *Engine) beginLive(title string) (*liveFeed, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.live != nil {
		return nil, false
	}
	f := &liveFeed{title: title, data: make(chan []byte, 32), stop: make(chan struct{})}
	e.live = f
	return f, true
}
func (e *Engine) endLive(f *liveFeed) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.live == f {
		e.live = nil
		close(f.stop)
	}
}
