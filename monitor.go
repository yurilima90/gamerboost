//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ---------- Monitor ao vivo (CPU / RAM / GPU / VRAM) ----------
// Um PowerShell fica rodando em segundo plano lendo os contadores de desempenho do Windows
// (classes CIM em inglês, funcionam em qualquer idioma) e escrevendo uma linha JSON por amostra.

const monitorScript = `
$ErrorActionPreference='SilentlyContinue'
$total=[math]::Round((Get-CimInstance Win32_OperatingSystem).TotalVisibleMemorySize/1024)
$base=(Get-CimInstance Win32_Processor | Select-Object -First 1).MaxClockSpeed
while($true){
  $pi=Get-CimInstance Win32_PerfFormattedData_Counters_ProcessorInformation -Filter "Name='_Total'"
  $cpu=$pi.PercentProcessorUtility
  if($cpu -eq $null){ $cpu=(Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor -Filter "Name='_Total'").PercentProcessorTime }
  $clk=$null; if($pi.PercentProcessorPerformance -ne $null){ $clk=[math]::Round($base*$pi.PercentProcessorPerformance/100) }
  $free=(Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory/1024
  $g=Get-CimInstance Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine | Where-Object { $_.Name -like '*engtype_3D*' }
  $gpu=($g | Measure-Object UtilizationPercentage -Sum).Sum
  $vm=(Get-CimInstance Win32_PerfFormattedData_GPUPerformanceCounters_GPUAdapterMemory | Measure-Object DedicatedUsage -Sum).Sum
  [pscustomobject]@{cpu=[math]::Min(100,[math]::Round([double]$cpu,1)); clock=$clk; ramUsed=[math]::Round($total-$free); ramTotal=$total;
    gpu=[math]::Min(100,[math]::Round([double]$gpu,1)); vram=[math]::Round([double]$vm/1MB)} | ConvertTo-Json -Compress
  [Console]::Out.Flush()
  Start-Sleep -Milliseconds 600
}`

var (
	monMu    sync.Mutex
	monLast  = []byte(`{"waiting":true}`)
	monReq   time.Time
	monCmd   *exec.Cmd
	monAlive bool
)

func monitorSample() []byte {
	monMu.Lock()
	monReq = time.Now()
	if !monAlive {
		monAlive = true
		go monitorLoop()
	}
	b := monLast
	monMu.Unlock()
	return b
}

func monitorLoop() {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", monitorScript)
	hidden(cmd)
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		monMu.Lock()
		monAlive = false
		monLast = []byte(`{"error":"monitor indisponível"}`)
		monMu.Unlock()
		return
	}
	monMu.Lock()
	monCmd = cmd
	monMu.Unlock()
	// encerra o PowerShell quando ninguém consulta há 20 s
	go func() {
		for {
			time.Sleep(5 * time.Second)
			monMu.Lock()
			idle := time.Since(monReq) > 20*time.Second
			alive := monAlive
			monMu.Unlock()
			if idle || !alive {
				cmd.Process.Kill()
				return
			}
		}
	}()
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) > 2 && line[0] == '{' && json.Valid(line) {
			monMu.Lock()
			monLast = append([]byte(nil), line...)
			monMu.Unlock()
		}
	}
	cmd.Wait()
	monMu.Lock()
	monAlive = false
	monMu.Unlock()
}

// ---------- Histórico de resultados ----------

type SavedRun struct {
	ID    int64           `json:"id"`
	Label string          `json:"label"`
	Time  string          `json:"time"`
	Data  json.RawMessage `json:"data"`
}

var (
	resMu sync.Mutex
)

func resPath() string { return filepath.Join(stateDir(), "benchmarks.json") }

func loadRuns() []SavedRun {
	var r []SavedRun
	if b, err := os.ReadFile(resPath()); err == nil {
		json.Unmarshal(b, &r)
	}
	if r == nil {
		r = []SavedRun{}
	}
	return r
}

func saveRun(label string, data json.RawMessage) SavedRun {
	resMu.Lock()
	defer resMu.Unlock()
	r := loadRuns()
	if len(label) > 60 {
		label = label[:60]
	}
	run := SavedRun{ID: time.Now().UnixNano(), Label: label, Time: time.Now().Format("02/01/2006 15:04"), Data: data}
	r = append(r, run)
	if len(r) > 100 {
		r = r[len(r)-100:]
	}
	b, _ := json.MarshalIndent(r, "", " ")
	os.WriteFile(resPath(), b, 0o644)
	return run
}

func deleteRun(id int64) {
	resMu.Lock()
	defer resMu.Unlock()
	r := loadRuns()
	out := r[:0]
	for _, x := range r {
		if x.ID != id {
			out = append(out, x)
		}
	}
	b, _ := json.MarshalIndent(out, "", " ")
	os.WriteFile(resPath(), b, 0o644)
}
