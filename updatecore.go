package main

// Núcleo do atualizador (independente de sistema, para poder ser testado).
// Só aceita arquivos publicados em Releases do repositório oficial deste projeto
// e só instala se o SHA-256 conferir.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// version é substituída na compilação (-ldflags "-X main.version=1.2.3").
var version = "3.2.0"

var (
	updateAPI     = "https://api.github.com/repos/yurilima90/gamerboost/releases/latest"
	allowedPrefix = "https://github.com/yurilima90/gamerboost/"
)

const maxExeSize = 60 << 20

type ReleaseInfo struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
	Notes   string `json:"notes"`
	ExeURL  string `json:"-"`
	ShaURL  string `json:"-"`
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// isNewer informa se latest é uma versão maior que current.
func isNewer(latest, current string) bool {
	a, ok1 := parseVersion(latest)
	b, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func fetchLatest(c *http.Client) (*ReleaseInfo, error) {
	req, _ := http.NewRequest("GET", updateAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GamerBoost/"+version)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("nenhuma versão publicada ainda")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub respondeu %d", resp.StatusCode)
	}
	var r struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	info := &ReleaseInfo{Tag: r.Tag, Version: strings.TrimPrefix(r.Tag, "v"), Notes: r.Body}
	for _, a := range r.Assets {
		switch a.Name {
		case "GamerBoost.exe":
			info.ExeURL = a.URL
		case "GamerBoost.exe.sha256":
			info.ShaURL = a.URL
		}
	}
	return info, nil
}

func parseSHA(txt string) (string, error) {
	f := strings.Fields(txt)
	if len(f) == 0 || len(f[0]) != 64 {
		return "", errors.New("arquivo de verificação inválido")
	}
	if _, err := hex.DecodeString(f[0]); err != nil {
		return "", errors.New("arquivo de verificação inválido")
	}
	return strings.ToLower(f[0]), nil
}

func get(c *http.Client, url string, limit int64) (io.ReadCloser, error) {
	if !strings.HasPrefix(url, allowedPrefix) {
		return nil, errors.New("endereço de download não permitido")
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "GamerBoost/"+version)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("download falhou (%d)", resp.StatusCode)
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, limit+1), resp.Body}, nil
}

// downloadVerified baixa o EXE para dest e confere SHA-256 e cabeçalho de executável.
func downloadVerified(c *http.Client, info *ReleaseInfo, dest string) error {
	if info.ExeURL == "" || info.ShaURL == "" {
		return errors.New("a versão publicada não tem o EXE e o arquivo .sha256")
	}
	sb, err := get(c, info.ShaURL, 4096)
	if err != nil {
		return err
	}
	txt, _ := io.ReadAll(sb)
	sb.Close()
	want, err := parseSHA(string(txt))
	if err != nil {
		return err
	}
	eb, err := get(c, info.ExeURL, maxExeSize)
	if err != nil {
		return err
	}
	defer eb.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), eb)
	f.Close()
	if err == nil && n > maxExeSize {
		err = errors.New("arquivo grande demais")
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = errors.New("verificação SHA-256 falhou: arquivo corrompido ou alterado")
	}
	if err == nil {
		if b, e := os.ReadFile(dest); e != nil || len(b) < 2 || b[0] != 'M' || b[1] != 'Z' {
			err = errors.New("o arquivo baixado não é um executável do Windows")
		}
	}
	if err != nil {
		os.Remove(dest)
	}
	return err
}
