//go:build windows

package main

import (
	"encoding/json"
	"time"
)

const scanScript = `
$ErrorActionPreference='SilentlyContinue'
function Fmt($d){ if($d){ try { return ([datetime]$d).ToString('yyyy-MM-dd') } catch { return '' } } return '' }

$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$cpuO = [pscustomobject]@{
  Name=($cpu.Name -replace '\s+',' ').Trim(); Cores=$cpu.NumberOfCores; Threads=$cpu.NumberOfLogicalProcessors
  MaxMHz=$cpu.MaxClockSpeed; CurMHz=$cpu.CurrentClockSpeed; L2KB=$cpu.L2CacheSize; L3KB=$cpu.L3CacheSize; Socket=$cpu.SocketDesignation }

$cls='HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}'
$gpuO = @(Get-CimInstance Win32_VideoController | ForEach-Object {
  $g=$_; $vram=$null
  Get-ChildItem $cls | Where-Object { $_.PSChildName -match '^\d{4}$' } | ForEach-Object {
    $p=Get-ItemProperty $_.PSPath
    if($p.DriverDesc -eq $g.Name){ $q=$p.'HardwareInformation.qwMemorySize'
      if($q -is [byte[]]){ $q=[BitConverter]::ToInt64($q,0) }
      if($q){ $vram=[int64]$q } } }
  if(-not $vram){ $vram=[int64]$g.AdapterRAM }
  [pscustomobject]@{ Name=$g.Name; VRAMMB=[math]::Round($vram/1MB); Driver=$g.DriverVersion; DriverDate=(Fmt $g.DriverDate)
    Resolution=("{0}x{1}" -f $g.CurrentHorizontalResolution,$g.CurrentVerticalResolution); Hz=$g.CurrentRefreshRate } })

$mods = @(Get-CimInstance Win32_PhysicalMemory | ForEach-Object {
  [pscustomobject]@{ Slot=$_.DeviceLocator; Type=$_.SMBIOSMemoryType; GB=[math]::Round($_.Capacity/1GB); Speed=$_.Speed; Configured=$_.ConfiguredClockSpeed
    Maker=($_.Manufacturer -replace '\s+',' ').Trim(); Part=($_.PartNumber -replace '\s+',' ').Trim() } })
$slots = (Get-CimInstance Win32_PhysicalMemoryArray | Measure-Object -Property MemoryDevices -Sum).Sum
$os = Get-CimInstance Win32_OperatingSystem
$board = Get-CimInstance Win32_BaseBoard | Select-Object -First 1
$bios = Get-CimInstance Win32_BIOS | Select-Object -First 1

$disks = @(Get-PhysicalDisk | ForEach-Object {
  [pscustomobject]@{ Name=$_.FriendlyName; Type=[string]$_.MediaType; Bus=[string]$_.BusType; SizeGB=[math]::Round($_.Size/1GB); Health=[string]$_.HealthStatus } })
$vols = @(Get-Volume | Where-Object { $_.DriveLetter -and $_.Size -gt 0 } | ForEach-Object {
  [pscustomobject]@{ Letter=[string]$_.DriveLetter; Label=$_.FileSystemLabel; SizeGB=[math]::Round($_.Size/1GB); FreeGB=[math]::Round($_.SizeRemaining/1GB) } })
$net = @(Get-NetAdapter -Physical | Where-Object Status -eq 'Up' | ForEach-Object {
  [pscustomobject]@{ Name=$_.InterfaceDescription; Speed=$_.LinkSpeed } })
$plan = ((powercfg /getactivescheme) -join ' ') -replace '^.*\((.*)\).*$','$1'

[pscustomobject]@{
  CPU=$cpuO; GPUs=$gpuO; RAM=[pscustomobject]@{ Modules=$mods; Slots=$slots; TotalGB=[math]::Round($os.TotalVisibleMemorySize/1MB,1); FreeGB=[math]::Round($os.FreePhysicalMemory/1MB,1) }
  Board=[pscustomobject]@{ Maker=$board.Manufacturer; Model=$board.Product; BIOS=$bios.SMBIOSBIOSVersion; BIOSDate=(Fmt $bios.ReleaseDate) }
  OS=[pscustomobject]@{ Name=$os.Caption; Version=$os.Version; Build=$os.BuildNumber; Arch=$os.OSArchitecture; UptimeH=[math]::Round(((Get-Date)-$os.LastBootUpTime).TotalHours,1) }
  Disks=$disks; Volumes=$vols; Network=$net; PowerPlan=$plan
  Startup=@(Get-CimInstance Win32_StartupCommand).Count; Processes=@(Get-Process).Count
} | ConvertTo-Json -Depth 6 -Compress
`

func scanPC() json.RawMessage {
	out, err := runPS(scanScript, 120*time.Second)
	if err != nil && len(out) == 0 {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return b
	}
	// extrai o JSON (ignora possíveis avisos antes do objeto)
	for i := 0; i < len(out); i++ {
		if out[i] == '{' {
			out = out[i:]
			break
		}
	}
	if !json.Valid([]byte(out)) {
		b, _ := json.Marshal(map[string]string{"error": "falha ao ler as especificações: " + firstLine(out)})
		return b
	}
	return json.RawMessage(out)
}
