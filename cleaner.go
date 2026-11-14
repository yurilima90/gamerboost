//go:build windows

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type cleanItem struct {
	ID      string
	Name    string
	Desc    string
	Globs   []string // caminhos (aceitam variáveis de ambiente e *)
	Default bool     // marcado por padrão
	Special string   // ação especial: recycle | dism | wu | dns
}

var cleanItems = []cleanItem{
	{ID: "temp-user", Name: "Arquivos temporários do usuário", Desc: "Pasta %TEMP% com restos de instaladores e jogos.", Default: true,
		Globs: []string{`%TEMP%`}},
	{ID: "temp-win", Name: "Temporários do Windows", Desc: "C:\\Windows\\Temp: arquivos que o sistema não precisa mais.", Default: true,
		Globs: []string{`%SystemRoot%\Temp`}},
	{ID: "wu-cache", Name: "Cache do Windows Update", Desc: "Arquivos de atualização já instalados (SoftwareDistribution).", Default: true, Special: "wu",
		Globs: []string{`%SystemRoot%\SoftwareDistribution\Download`}},
	{ID: "delivopt", Name: "Cache de Otimização de Entrega", Desc: "Pedaços de atualização guardados para compartilhar.", Default: true,
		Globs: []string{`%SystemRoot%\ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache`}},
	{ID: "thumbs", Name: "Cache de miniaturas", Desc: "Miniaturas do Explorer (são recriadas sozinhas).", Default: true,
		Globs: []string{`%LOCALAPPDATA%\Microsoft\Windows\Explorer\thumbcache_*.db`, `%LOCALAPPDATA%\Microsoft\Windows\Explorer\iconcache_*.db`}},
	{ID: "wer", Name: "Relatórios de erro e dumps", Desc: "Relatórios de erro, minidumps e memory dump antigos.", Default: true,
		Globs: []string{`%ProgramData%\Microsoft\Windows\WER\ReportQueue`, `%ProgramData%\Microsoft\Windows\WER\ReportArchive`,
			`%LOCALAPPDATA%\CrashDumps`, `%SystemRoot%\Minidump`, `%SystemRoot%\MEMORY.DMP`}},
	{ID: "winlogs", Name: "Logs do Windows", Desc: "Logs de instalação e do CBS.", Default: true,
		Globs: []string{`%SystemRoot%\Logs\CBS\*.log`, `%SystemRoot%\Logs\CBS\*.cab`, `%SystemRoot%\Logs\DISM\*.log`}},
	{ID: "chrome", Name: "Cache do Google Chrome", Desc: "Cache, Code Cache e GPUCache (senhas e histórico não são tocados).", Default: true,
		Globs: []string{`%LOCALAPPDATA%\Google\Chrome\User Data\*\Cache\Cache_Data`, `%LOCALAPPDATA%\Google\Chrome\User Data\*\Code Cache`, `%LOCALAPPDATA%\Google\Chrome\User Data\*\GPUCache`}},
	{ID: "edge", Name: "Cache do Microsoft Edge", Desc: "Cache, Code Cache e GPUCache do Edge.", Default: true,
		Globs: []string{`%LOCALAPPDATA%\Microsoft\Edge\User Data\*\Cache\Cache_Data`, `%LOCALAPPDATA%\Microsoft\Edge\User Data\*\Code Cache`, `%LOCALAPPDATA%\Microsoft\Edge\User Data\*\GPUCache`}},
	{ID: "firefox", Name: "Cache do Firefox", Desc: "Cache de navegação do Firefox.", Default: true,
		Globs: []string{`%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\cache2`}},
	{ID: "launchers", Name: "Cache de Discord, Steam e Epic", Desc: "Caches de interface dos launchers (jogos e logins permanecem).", Default: true,
		Globs: []string{`%APPDATA%\discord\Cache`, `%APPDATA%\discord\Code Cache`, `%APPDATA%\discord\GPUCache`,
			`%LOCALAPPDATA%\Steam\htmlcache`, `%LOCALAPPDATA%\EpicGamesLauncher\Saved\webcache*`}},
	{ID: "recycle", Name: "Lixeira", Desc: "Esvazia a Lixeira de todos os discos.", Default: true, Special: "recycle"},
	{ID: "dns", Name: "Cache de DNS", Desc: "Limpa o cache de nomes de rede (resolve falhas de conexão).", Default: true, Special: "dns"},
	{ID: "shaders", Name: "Cache de shaders (DirectX/AMD)", Desc: "Libera espaço, mas os jogos vão recompilar shaders e podem engasgar na 1ª abertura. Use só se houver problemas gráficos.", Default: false,
		Globs: []string{`%LOCALAPPDATA%\D3DSCache`, `%LOCALAPPDATA%\AMD\DxCache`, `%LOCALAPPDATA%\AMD\DxcCache`, `%LOCALAPPDATA%\AMD\GLCache`}},
	{ID: "dism", Name: "Limpeza de componentes do Windows (DISM)", Desc: "Remove versões antigas de componentes do sistema. Demora alguns minutos.", Default: false, Special: "dism"},
}

type CleanInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Bytes   int64  `json:"bytes"`
	Files   int    `json:"files"`
	Default bool   `json:"default"`
	Special bool   `json:"special"`
}

type CleanResult struct {
	ID    string `json:"id"`
	Freed int64  `json:"freed"`
	Msg   string `json:"msg"`
}

// expandEnvPercent expande %VAR% no estilo do Windows.
func expandEnvPercent(s string) string {
	for {
		i := strings.Index(s, "%")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+1:], "%")
		if j < 0 {
			return s
		}
		name := s[i+1 : i+1+j]
		s = s[:i] + os.Getenv(name) + s[i+j+2:]
	}
}

func resolve(globs []string) []string {
	var out []string
	for _, g := range globs {
		p := expandEnvPercent(g)
		if strings.HasPrefix(p, `\`) || len(filepath.VolumeName(p)) == 0 {
			continue // variável vazia: ignora por segurança
		}
		if strings.Contains(p, "*") {
			m, _ := filepath.Glob(p)
			out = append(out, m...)
		} else if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func safePath(p string) bool {
	p = filepath.Clean(p)
	vol := filepath.VolumeName(p)
	rest := strings.Trim(strings.TrimPrefix(p, vol), `\`)
	return rest != "" && strings.Count(rest, `\`) >= 1
}

func sizeOf(p string) (int64, int) {
	var total int64
	n := 0
	st, err := os.Stat(p)
	if err != nil {
		return 0, 0
	}
	if !st.IsDir() {
		return st.Size(), 1
	}
	filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
				n++
			}
		}
		return nil
	})
	return total, n
}

func cleanScan() []CleanInfo {
	out := []CleanInfo{}
	for _, it := range cleanItems {
		ci := CleanInfo{ID: it.ID, Name: it.Name, Desc: it.Desc, Default: it.Default, Special: it.Globs == nil}
		for _, p := range resolve(it.Globs) {
			if !safePath(p) {
				continue
			}
			b, n := sizeOf(p)
			ci.Bytes += b
			ci.Files += n
		}
		if it.Special == "recycle" {
			ci.Bytes, ci.Files = recycleSize()
		}
		out = append(out, ci)
	}
	return out
}

func recycleSize() (int64, int) {
	out, err := runPS(`$s=(New-Object -ComObject Shell.Application).NameSpace(0xA).Items(); $t=0; foreach($i in $s){ $t+=$i.Size }; Write-Output "$t $($s.Count)"`, 40*time.Second)
	if err != nil {
		return 0, 0
	}
	var b int64
	var n int
	parts := strings.Fields(out)
	if len(parts) == 2 {
		b = atoi64(parts[0])
		n = int(atoi64(parts[1]))
	}
	return b, n
}

func atoi64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// removeContents apaga o conteúdo de uma pasta (mantém a pasta); arquivos em uso são ignorados.
func removeContents(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

func cleanRun(ids []string) []CleanResult {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	res := []CleanResult{}
	for _, it := range cleanItems {
		if !want[it.ID] {
			continue
		}
		r := CleanResult{ID: it.ID, Msg: "limpo"}
		if it.Special == "wu" {
			run(60*time.Second, "net.exe", "stop", "wuauserv")
			run(60*time.Second, "net.exe", "stop", "bits")
		}
		for _, p := range resolve(it.Globs) {
			if !safePath(p) {
				continue
			}
			b, _ := sizeOf(p)
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if st.IsDir() {
				removeContents(p)
			} else {
				os.Remove(p)
			}
			a, _ := sizeOf(p)
			if b > a {
				r.Freed += b - a
			}
		}
		switch it.Special {
		case "wu":
			run(60*time.Second, "net.exe", "start", "bits")
			run(60*time.Second, "net.exe", "start", "wuauserv")
		case "recycle":
			b, _ := recycleSize()
			runPS(`Clear-RecycleBin -Force -ErrorAction SilentlyContinue`, 2*time.Minute)
			r.Freed += b
		case "dns":
			run(30*time.Second, "ipconfig.exe", "/flushdns")
		case "dism":
			if out, err := run(15*time.Minute, "dism.exe", "/Online", "/Cleanup-Image", "/StartComponentCleanup"); err != nil {
				r.Msg = firstLine(out)
			}
		}
		res = append(res, r)
	}
	return res
}
