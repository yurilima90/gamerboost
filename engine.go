//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------- Tipos ----------

// Action descreve uma alteração do sistema.
//
//	reg    : grava valor de registro (Key, Name, Type, Value)
//	regdel : remove valor de registro (Key, Name)
//	svc    : muda o tipo de início de um serviço (Name, Value = disabled|demand|auto)
//	ps     : roda PowerShell (Value = script)
//	cmd    : roda cmd.exe (Value = comando)
type Action struct {
	Kind  string
	Key   string
	Name  string
	Type  string
	Value string
}

type Tweak struct {
	ID       string
	Cat      string
	Name     string
	Desc     string
	Risk     string // "seguro" | "moderado"
	Reboot   bool
	NoRevert bool // não dá para desfazer automaticamente (ex.: remoção de apps)
	Do       []Action
	Undo     []Action // desfazer explícito para ps/cmd (reg/svc são restaurados automaticamente)
}

type TweakView struct {
	ID       string `json:"id"`
	Cat      string `json:"cat"`
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Risk     string `json:"risk"`
	Reboot   bool   `json:"reboot"`
	Applied  bool   `json:"applied"`
	NoRevert bool   `json:"norevert"`
}

type Result struct {
	ID  string `json:"id"`
	OK  bool   `json:"ok"`
	Msg string `json:"msg"`
}

// undoEntry guarda o estado anterior para poder reverter.
type undoEntry struct {
	Kind    string `json:"kind"` // reg | svc | ps | cmd
	Key     string `json:"key,omitempty"`
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Data    string `json:"data,omitempty"`
	Existed bool   `json:"existed,omitempty"`
	Cmd     string `json:"cmd,omitempty"`
}

var (
	stMu    sync.Mutex
	applied = map[string][]undoEntry{}
	stPath  string
)

func stateDir() string {
	d := filepath.Join(os.Getenv("ProgramData"), "GamerBoost")
	os.MkdirAll(d, 0o755)
	return d
}

func loadState() {
	stPath = filepath.Join(stateDir(), "state.json")
	if b, err := os.ReadFile(stPath); err == nil {
		json.Unmarshal(b, &applied)
	}
}

func saveState() {
	b, _ := json.MarshalIndent(applied, "", " ")
	os.WriteFile(stPath, b, 0o644)
}

// ---------- Execução de comandos ----------

func hidden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func run(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	hidden(cmd)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// runCmd executa uma linha de cmd.exe passando a linha de comando crua (/S /C "..."),
// evitando problemas de aspas e do operador & .
func runCmd(line string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000,
		CmdLine: `cmd.exe /S /C "` + line + `"`}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

func runPS(script string, timeout time.Duration) (string, error) {
	return run(timeout, "powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command",
		"$ProgressPreference='SilentlyContinue';$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.Encoding]::UTF8;"+script)
}

// ---------- Registro / serviços ----------

var regLine = regexp.MustCompile(`^\s+(.+?)\s+(REG_[A-Z_]+)\s*(.*)$`)

func regRead(key, name string) (typ, data string, ok bool) {
	out, err := run(20*time.Second, "reg.exe", "query", key, "/v", name)
	if err != nil {
		return "", "", false
	}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		m := regLine.FindStringSubmatch(ln)
		if m != nil && strings.EqualFold(m[1], name) {
			typ, data = m[2], strings.TrimSpace(m[3])
			if typ == "REG_DWORD" || typ == "REG_QWORD" {
				if v, e := strconv.ParseUint(strings.TrimPrefix(data, "0x"), 16, 64); e == nil {
					data = strconv.FormatUint(v, 10)
				}
			}
			return typ, data, true
		}
	}
	return "", "", false
}

func regWrite(key, name, typ, data string) (string, error) {
	return run(20*time.Second, "reg.exe", "add", key, "/v", name, "/t", typ, "/d", data, "/f")
}

func regDelete(key, name string) (string, error) {
	return run(20*time.Second, "reg.exe", "delete", key, "/v", name, "/f")
}

var startRe = regexp.MustCompile(`START_TYPE\s*:\s*(\d)`)

func svcStart(name string) (string, bool) {
	out, err := run(20*time.Second, "sc.exe", "qc", name)
	if err != nil {
		return "", false
	}
	m := startRe.FindStringSubmatch(out)
	if m == nil {
		return "", false
	}
	switch m[1] {
	case "2":
		if strings.Contains(out, "DELAYED") {
			return "delayed-auto", true
		}
		return "auto", true
	case "3":
		return "demand", true
	case "4":
		return "disabled", true
	case "1":
		return "system", true
	}
	return "", false
}

func svcSet(name, mode string) (string, error) {
	out, err := run(30*time.Second, "sc.exe", "config", name, "start=", mode)
	if err == nil && mode == "disabled" {
		run(30*time.Second, "sc.exe", "stop", name)
	}
	return out, err
}

// ---------- Aplicar / reverter ----------

func findTweak(id string) *Tweak {
	for i := range catalog {
		if catalog[i].ID == id {
			return &catalog[i]
		}
	}
	return nil
}

func listTweaks() []TweakView {
	stMu.Lock()
	defer stMu.Unlock()
	out := make([]TweakView, 0, len(catalog))
	for _, t := range catalog {
		_, ok := applied[t.ID]
		out = append(out, TweakView{t.ID, t.Cat, t.Name, t.Desc, t.Risk, t.Reboot, ok, t.NoRevert})
	}
	return out
}

func applyTweak(id string) Result {
	t := findTweak(id)
	if t == nil {
		return Result{id, false, "ajuste desconhecido"}
	}
	stMu.Lock()
	if _, ok := applied[id]; ok {
		stMu.Unlock()
		return Result{id, true, "já aplicado"}
	}
	stMu.Unlock()

	var undos []undoEntry
	var errs []string
	for _, a := range t.Do {
		switch a.Kind {
		case "reg":
			typ, data, existed := regRead(a.Key, a.Name)
			undos = append(undos, undoEntry{Kind: "reg", Key: a.Key, Name: a.Name, Type: typ, Data: data, Existed: existed})
			if out, err := regWrite(a.Key, a.Name, a.Type, a.Value); err != nil {
				errs = append(errs, a.Name+": "+firstLine(out))
			}
		case "regdel":
			typ, data, existed := regRead(a.Key, a.Name)
			if !existed {
				continue
			}
			undos = append(undos, undoEntry{Kind: "reg", Key: a.Key, Name: a.Name, Type: typ, Data: data, Existed: true})
			if out, err := regDelete(a.Key, a.Name); err != nil {
				errs = append(errs, a.Name+": "+firstLine(out))
			}
		case "svc":
			prev, ok := svcStart(a.Name)
			if !ok { // serviço não existe neste Windows: ignora
				continue
			}
			undos = append(undos, undoEntry{Kind: "svc", Name: a.Name, Data: prev})
			if out, err := svcSet(a.Name, a.Value); err != nil {
				errs = append(errs, a.Name+": "+firstLine(out))
			}
		case "ps":
			if out, err := runPS(a.Value, 5*time.Minute); err != nil {
				errs = append(errs, firstLine(out))
			}
		case "cmd":
			if out, err := runCmd(a.Value); err != nil {
				errs = append(errs, firstLine(out))
			}
		}
	}
	for _, u := range t.Undo {
		undos = append(undos, undoEntry{Kind: u.Kind, Cmd: u.Value})
	}
	stMu.Lock()
	applied[id] = undos
	saveState()
	stMu.Unlock()
	if len(errs) > 0 {
		return Result{id, true, "aplicado com avisos: " + strings.Join(errs, "; ")}
	}
	return Result{id, true, "aplicado"}
}

func revertTweak(id string) Result {
	if t := findTweak(id); t != nil && t.NoRevert {
		return Result{id, false, "não reversível automaticamente: reinstale o app pela Microsoft Store"}
	}
	stMu.Lock()
	undos, ok := applied[id]
	stMu.Unlock()
	if !ok {
		return Result{id, true, "não estava aplicado"}
	}
	var errs []string
	for i := len(undos) - 1; i >= 0; i-- {
		u := undos[i]
		switch u.Kind {
		case "reg":
			var out string
			var err error
			if u.Existed {
				out, err = regWrite(u.Key, u.Name, u.Type, u.Data)
			} else {
				out, err = regDelete(u.Key, u.Name)
			}
			if err != nil && u.Existed {
				errs = append(errs, firstLine(out))
			}
		case "svc":
			mode := u.Data
			switch mode {
			case "delayed-auto":
				mode = "delayed-auto"
			case "system":
				mode = "system"
			}
			if out, err := svcSet(u.Name, mode); err != nil {
				errs = append(errs, firstLine(out))
			} else if mode == "auto" || mode == "delayed-auto" {
				run(30*time.Second, "sc.exe", "start", u.Name)
			}
		case "ps":
			if out, err := runPS(u.Cmd, 5*time.Minute); err != nil {
				errs = append(errs, firstLine(out))
			}
		case "cmd":
			if out, err := runCmd(u.Cmd); err != nil {
				errs = append(errs, firstLine(out))
			}
		}
	}
	stMu.Lock()
	delete(applied, id)
	saveState()
	stMu.Unlock()
	if len(errs) > 0 {
		return Result{id, true, "revertido com avisos: " + strings.Join(errs, "; ")}
	}
	return Result{id, true, "revertido"}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		s = s[:i]
	}
	if s == "" {
		return "falhou"
	}
	return s
}

// ---------- Ponto de restauração ----------

func createRestorePoint() Result {
	script := `
Enable-ComputerRestore -Drive "$($env:SystemDrive)\"
New-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore' -Name 'SystemRestorePointCreationFrequency' -Value 0 -PropertyType DWord -Force | Out-Null
Checkpoint-Computer -Description 'GamerBoost - antes das otimizacoes' -RestorePointType MODIFY_SETTINGS
Write-Output 'ok'`
	out, err := runPS(script, 5*time.Minute)
	if err != nil {
		return Result{"restore", false, firstLine(out)}
	}
	// Backup extra do registro das áreas mais mexidas.
	d := filepath.Join(stateDir(), "backup-"+time.Now().Format("20060102-150405"))
	os.MkdirAll(d, 0o755)
	for n, k := range map[string]string{
		"gamebar.reg":  `HKCU\Software\Microsoft\GameBar`,
		"gamedvr.reg":  `HKCU\System\GameConfigStore`,
		"mmcss.reg":    `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`,
		"policies.reg": `HKLM\SOFTWARE\Policies\Microsoft\Windows`,
	} {
		run(60*time.Second, "reg.exe", "export", k, filepath.Join(d, n), "/y")
	}
	return Result{"restore", true, "Ponto de restauração criado. Backup do registro em " + d}
}

var _ = fmt.Sprintf
