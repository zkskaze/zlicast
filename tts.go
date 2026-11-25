package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (a *App) ttsRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/news/speech", a.permit("admin,editor,dj", func(w http.ResponseWriter, r *http.Request, user string) {
		var v struct{ Title, Text, Station, Expires string }
		if decode(r, &v) != nil || strings.TrimSpace(v.Text) == "" || len([]rune(v.Text)) > 6000 {
			fail(w, 400, "Унеси текст до 6.000 знакова.")
			return
		}
		if strings.TrimSpace(v.Title) == "" {
			v.Title = "Вести"
		}
		dir, err := os.MkdirTemp(a.data, ".speech-")
		if err != nil {
			fail(w, 500, "Није могуће припремити снимак.")
			return
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "speech.wav")
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			const script = `$ErrorActionPreference='Stop'; [Console]::InputEncoding=[Text.UTF8Encoding]::new($false); Add-Type -AssemblyName System.Speech; $s=[System.Speech.Synthesis.SpeechSynthesizer]::new(); try { $voice=$s.GetInstalledVoices() | Where-Object { $_.Enabled -and $_.VoiceInfo.Culture.TwoLetterISOLanguageName -eq 'sr' } | Select-Object -First 1; if ($voice) { $s.SelectVoice($voice.VoiceInfo.Name) }; $s.SetOutputToWaveFile($env:ZLICAST_SPEECH_FILE); $s.Speak([Console]::In.ReadToEnd()) } finally { $s.Dispose() }`
			cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
			cmd.Env = append(os.Environ(), "ZLICAST_SPEECH_FILE="+path)
		} else {
			cmd = exec.CommandContext(ctx, "espeak-ng", "-v", "sr", "-w", path, "--stdin")
		}
		cmd.Stdin = strings.NewReader(v.Text)
		if err = cmd.Run(); err != nil {
			fail(w, 503, "Синтеза говора тренутно није доступна. Провери инсталиране системске гласове.")
			return
		}
		file, err := os.Open(path)
		if err != nil {
			fail(w, 503, "Глас није генерисао снимак.")
			return
		}
		defer file.Close()
		m, dup, err := a.addMedia(ctx, user, file, fmt.Sprintf("%s.wav", v.Title), "news", v.Station, "TTS", v.Expires)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]any{"media": m, "duplicate": dup})
	}))
}
