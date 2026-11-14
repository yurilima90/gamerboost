package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		l, c string
		w    bool
	}{
		{"v3.2.0", "3.1.0", true}, {"3.1.1", "3.1.0", true}, {"4.0.0", "3.9.9", true},
		{"3.1.0", "3.1.0", false}, {"3.0.9", "3.1.0", false}, {"v3.10.0", "3.9.0", true},
		{"abc", "3.1.0", false}, {"3.1.0", "dev", false}, {"3.2", "3.1.0", true},
	}
	for _, c := range cases {
		if got := isNewer(c.l, c.c); got != c.w {
			t.Errorf("isNewer(%q,%q)=%v quer %v", c.l, c.c, got, c.w)
		}
	}
}

func TestFetchAndDownload(t *testing.T) {
	exe := append([]byte("MZ"), make([]byte, 5000)...)
	sum := sha256.Sum256(exe)
	good := hex.EncodeToString(sum[:])
	sha := good
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rel":
			fmt.Fprintf(w, `{"tag_name":"v9.9.9","body":"notas","assets":[{"name":"GamerBoost.exe","browser_download_url":"%s/dl/GamerBoost.exe"},{"name":"GamerBoost.exe.sha256","browser_download_url":"%s/dl/GamerBoost.exe.sha256"}]}`, srv.URL, srv.URL)
		case "/dl/GamerBoost.exe":
			w.Write(exe)
		case "/dl/GamerBoost.exe.sha256":
			fmt.Fprintf(w, "%s  GamerBoost.exe\n", sha)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	updateAPI, allowedPrefix = srv.URL+"/rel", srv.URL+"/dl/"
	c := &http.Client{Timeout: 5 * time.Second}
	info, err := fetchLatest(c)
	if err != nil || info.Version != "9.9.9" || info.ExeURL == "" || info.ShaURL == "" {
		t.Fatalf("fetchLatest: %v %+v", err, info)
	}
	dest := filepath.Join(t.TempDir(), "new.exe")
	if err := downloadVerified(c, info, dest); err != nil {
		t.Fatal(err)
	}
	// hash errado deve ser recusado e o arquivo removido
	sha = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := downloadVerified(c, info, dest); err == nil {
		t.Fatal("deveria falhar com SHA errado")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("arquivo inválido não foi removido")
	}
	// URL fora do repositório oficial deve ser recusada
	bad := &ReleaseInfo{ExeURL: "https://evil.example/x.exe", ShaURL: "https://evil.example/x.sha256"}
	if err := downloadVerified(c, bad, dest); err == nil {
		t.Fatal("deveria recusar URL fora do repositório")
	}
	// executável sem cabeçalho MZ
	exe = []byte("NOTANEXE")
	s2 := sha256.Sum256(exe)
	sha = hex.EncodeToString(s2[:])
	if err := downloadVerified(c, info, dest); err == nil {
		t.Fatal("deveria recusar arquivo que não é executável")
	}
}
