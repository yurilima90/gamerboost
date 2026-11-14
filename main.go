//go:build windows

package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

//go:embed ui.html
var uiHTML string

var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	procIsAdmin   = shell32.NewProc("IsUserAnAdmin")
	procShellExec = shell32.NewProc("ShellExecuteW")
	user32        = syscall.NewLazyDLL("user32.dll")
	procMsgBox    = user32.NewProc("MessageBoxW")
	token         string
	lastPing      atomic.Int64
)

func msgBox(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	procMsgBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

func isAdmin() bool {
	r, _, _ := procIsAdmin.Call()
	return r != 0
}

// relaunchAsAdmin pede elevação (UAC) e reabre o próprio programa.
func relaunchAsAdmin() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	r, _, _ := procShellExec.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
	return r > 32
}

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// guard bloqueia chamadas que não venham da nossa própria interface
// (token secreto + Host local, contra DNS rebinding e páginas maliciosas).
func guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if !strings.HasPrefix(host, "127.0.0.1:") {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.Header.Get("X-GB-Token") != token {
			http.Error(w, "forbidden", 403)
			return
		}
		h(w, r)
	}
}

type idsReq struct {
	IDs []string `json:"ids"`
}

func main() {
	if !isAdmin() {
		if !relaunchAsAdmin() {
			msgBox("GamerBoost", "Não foi possível obter permissão de administrador.\nClique com o botão direito no programa > Executar como administrador.")
		}
		return
	}
	loadState()
	cleanupOld()
	token = newToken()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		msgBox("GamerBoost", "Erro ao iniciar: "+err.Error())
		return
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || !strings.HasPrefix(r.Host, "127.0.0.1:") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, strings.Replace(uiHTML, "__TOKEN__", token, 1))
	})
	mux.HandleFunc("/api/ping", guard(func(w http.ResponseWriter, r *http.Request) {
		lastPing.Store(time.Now().Unix())
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/tweaks", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, listTweaks())
	}))
	mux.HandleFunc("/api/apply", guard(func(w http.ResponseWriter, r *http.Request) {
		var q idsReq
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q)
		out := []Result{}
		for _, id := range q.IDs {
			out = append(out, applyTweak(id))
		}
		writeJSON(w, out)
	}))
	mux.HandleFunc("/api/revert", guard(func(w http.ResponseWriter, r *http.Request) {
		var q idsReq
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q)
		out := []Result{}
		for _, id := range q.IDs {
			out = append(out, revertTweak(id))
		}
		writeJSON(w, out)
	}))
	mux.HandleFunc("/api/scan", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, scanPC())
	}))
	mux.HandleFunc("/api/clean/scan", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, cleanScan())
	}))
	mux.HandleFunc("/api/clean/run", guard(func(w http.ResponseWriter, r *http.Request) {
		var q idsReq
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q)
		writeJSON(w, cleanRun(q.IDs))
	}))
	mux.HandleFunc("/api/restore/create", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, createRestorePoint())
	}))
	mux.HandleFunc("/api/restore/open", guard(func(w http.ResponseWriter, r *http.Request) {
		exec.Command("rstrui.exe").Start()
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/monitor", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(monitorSample())
	}))
	mux.HandleFunc("/api/bench/run", guard(func(w http.ResponseWriter, r *http.Request) {
		stopBench.Store(false)
		switch r.URL.Query().Get("test") {
		case "single":
			writeJSON(w, BenchSingle())
		case "multi":
			writeJSON(w, BenchMulti())
		case "ram":
			writeJSON(w, BenchRAM())
		case "latency":
			writeJSON(w, BenchLatency())
		default:
			http.Error(w, "teste desconhecido", 400)
		}
	}))
	mux.HandleFunc("/api/bench/stress", guard(func(w http.ResponseWriter, r *http.Request) {
		stopBench.Store(false)
		sec, _ := strconv.Atoi(r.URL.Query().Get("seconds"))
		writeJSON(w, BenchStress(sec))
	}))
	mux.HandleFunc("/api/bench/stop", guard(func(w http.ResponseWriter, r *http.Request) {
		stopBench.Store(true)
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/results", guard(func(w http.ResponseWriter, r *http.Request) {
		resMu.Lock()
		defer resMu.Unlock()
		writeJSON(w, loadRuns())
	}))
	mux.HandleFunc("/api/results/save", guard(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Label string          `json:"label"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&q); err != nil || !json.Valid(q.Data) {
			http.Error(w, "inválido", 400)
			return
		}
		writeJSON(w, saveRun(q.Label, q.Data))
	}))
	mux.HandleFunc("/api/results/delete", guard(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&q)
		deleteRun(q.ID)
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/update/check", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, checkUpdate())
	}))
	mux.HandleFunc("/api/update/apply", guard(func(w http.ResponseWriter, r *http.Request) {
		msg, err := applyUpdate()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "msg": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "msg": msg, "restart": strings.Contains(msg, "reiniciar")})
		if strings.Contains(msg, "reiniciar") {
			go func() { time.Sleep(1500 * time.Millisecond); os.Exit(0) }()
		}
	}))
	mux.HandleFunc("/api/exit", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		go func() { time.Sleep(200 * time.Millisecond); os.Exit(0) }()
	}))

	go http.Serve(ln, mux)

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	browser := openUI(url)
	var browserDone atomic.Bool
	if browser != nil {
		go func() { browser.Wait(); browserDone.Store(true) }()
	} else {
		browserDone.Store(true)
	}

	// Só encerra quando a janela realmente foi fechada:
	//  - o navegador terminou e ficou 60 s sem sinal; ou
	//  - ficou 30 min sem nenhum sinal (janela em segundo plano é normal, não fecha antes).
	start := time.Now()
	lastPing.Store(time.Now().Unix())
	for {
		time.Sleep(5 * time.Second)
		idle := time.Now().Unix() - lastPing.Load()
		if browserDone.Load() && idle > 60 && time.Since(start) > 20*time.Second {
			return
		}
		if idle > 30*60 {
			return
		}
	}
}

// openUI abre uma janela de aplicativo (Edge/Chrome em modo --app) ou o navegador padrão.
func openUI(url string) *exec.Cmd {
	cands := []string{
		os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
		os.Getenv("ProgramFiles(x86)") + `\Google\Chrome\Application\chrome.exe`,
	}
	profile := os.Getenv("ProgramData") + `\GamerBoost\browser`
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			cmd := exec.Command(c, "--app="+url, "--user-data-dir="+profile,
				"--window-size=1180,780", "--no-first-run", "--disable-extensions")
			if cmd.Start() == nil {
				return cmd
			}
		}
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	return nil
}
