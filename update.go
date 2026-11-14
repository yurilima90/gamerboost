//go:build windows

package main

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var updMu sync.Mutex

func updClient() *http.Client { return &http.Client{Timeout: 5 * time.Minute} }

func selfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return exe
}

// cleanupOld apaga o EXE antigo deixado por uma atualização anterior.
func cleanupOld() {
	if exe := selfPath(); exe != "" {
		go func() {
			time.Sleep(4 * time.Second)
			os.Remove(exe + ".old")
			os.Remove(exe + ".new")
		}()
	}
}

func checkUpdate() map[string]any {
	out := map[string]any{"current": version, "newer": false}
	c := &http.Client{Timeout: 15 * time.Second}
	info, err := fetchLatest(c)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["latest"] = info.Version
	out["notes"] = info.Notes
	out["newer"] = isNewer(info.Version, version) && info.ExeURL != "" && info.ShaURL != ""
	return out
}

// applyUpdate baixa a versão nova, confere o SHA-256 e troca o EXE.
// O Windows não deixa apagar um EXE em uso, mas permite renomeá-lo: o atual vira .old.
func applyUpdate() (string, error) {
	updMu.Lock()
	defer updMu.Unlock()
	exe := selfPath()
	if exe == "" {
		return "", errors.New("não foi possível localizar o programa")
	}
	info, err := fetchLatest(updClient())
	if err != nil {
		return "", err
	}
	if !isNewer(info.Version, version) {
		return "Você já está na versão mais recente (" + version + ").", nil
	}
	newPath, oldPath := exe+".new", exe+".old"
	if err := downloadVerified(updClient(), info, newPath); err != nil {
		return "", err
	}
	os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		os.Remove(newPath)
		return "", errors.New("não foi possível substituir o programa (ele está numa pasta protegida?): " + err.Error())
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(oldPath, exe)
		os.Remove(newPath)
		return "", err
	}
	if err := exec.Command(exe).Start(); err != nil {
		return "", errors.New("atualizado, mas não consegui reabrir: abra o GamerBoost.exe manualmente")
	}
	return "Atualizado para a versão " + info.Version + ". O programa vai reiniciar.", nil
}
