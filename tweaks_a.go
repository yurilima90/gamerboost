//go:build windows

package main

// Catálogo de ajustes — parte A: desempenho, latência/entrada, CPU Ryzen, GPU RX 580.
// Tudo é gratuito e feito só com recursos nativos do Windows (registro, powercfg, bcdedit, serviços).

var catalog []Tweak

func R(key, name, typ, val string) Action {
	return Action{Kind: "reg", Key: key, Name: name, Type: typ, Value: val}
}
func D(key, name string) Action  { return Action{Kind: "regdel", Key: key, Name: name} }
func S(name, mode string) Action { return Action{Kind: "svc", Name: name, Value: mode} }
func P(script string) Action     { return Action{Kind: "ps", Value: script} }
func C(cmd string) Action        { return Action{Kind: "cmd", Value: cmd} }

// pw monta um comando powercfg para o plano de energia ativo (AC e bateria).
func pw(sub, setting, val string) string {
	return "powercfg -setacvalueindex SCHEME_CURRENT " + sub + " " + setting + " " + val +
		" & powercfg -setdcvalueindex SCHEME_CURRENT " + sub + " " + setting + " " + val +
		" & powercfg -setactive SCHEME_CURRENT"
}

const (
	catPerf  = "Desempenho"
	catInput = "Latência e Entrada"
	catCPU   = "CPU Ryzen 5 1600"
	catGPU   = "GPU RX 580"
	catNet   = "Rede e Ping"
	catSvc   = "Serviços"
	catBloat = "Bloatware"
	catPriv  = "Privacidade e Telemetria"
	catUI    = "Interface Leve"
)

const (
	gcs   = `HKCU\System\GameConfigStore`
	mmcss = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`
	cdm   = `HKCU\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager`
	desk  = `HKCU\Control Panel\Desktop`
	expAd = `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`
)

func init() {
	catalog = append(catalog,

		// ===================== DESEMPENHO =====================
		Tweak{ID: "perf-ultimate", Cat: catPerf, Risk: "seguro",
			Name: "Plano de energia Desempenho Máximo",
			Desc: "Cria e ativa o plano \"Ultimate Performance\": CPU sempre pronta, sem economia de energia que causa micro-travadas.",
			Do:   []Action{P(`$o = powercfg -duplicatescheme e9a42b02-d5df-448d-aa00-03f14749eb61; $g=[regex]::Match(($o -join ' '),'[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}').Value; if(-not $g){throw 'falha ao criar plano'}; powercfg -setactive $g`)},
			Undo: []Action{C(`powercfg -setactive 381b4222-f694-41f0-9685-ff5bb260df2e`)}},

		Tweak{ID: "perf-gamedvr", Cat: catPerf, Risk: "seguro",
			Name: "Desativar Game DVR (gravação em segundo plano)",
			Desc: "Impede o Windows de gravar o jogo em segundo plano, liberando CPU/GPU e removendo quedas de FPS.",
			Do: []Action{
				R(gcs, "GameDVR_Enabled", "REG_DWORD", "0"),
				R(`HKCU\Software\Microsoft\Windows\CurrentVersion\GameDVR`, "AppCaptureEnabled", "REG_DWORD", "0"),
				R(`HKLM\SOFTWARE\Policies\Microsoft\Windows\GameDVR`, "AllowGameDVR", "REG_DWORD", "0")}},

		Tweak{ID: "perf-gamemode", Cat: catPerf, Risk: "seguro",
			Name: "Ativar Modo de Jogo",
			Desc: "Dá prioridade ao jogo em primeiro plano e evita que o Windows Update instale drivers/reinicie durante a partida.",
			Do: []Action{
				R(`HKCU\Software\Microsoft\GameBar`, "AutoGameModeEnabled", "REG_DWORD", "1"),
				R(`HKCU\Software\Microsoft\GameBar`, "AllowAutoGameMode", "REG_DWORD", "1")}},

		Tweak{ID: "perf-gamebar", Cat: catPerf, Risk: "seguro",
			Name: "Desativar Xbox Game Bar (atalhos e dicas)",
			Desc: "Remove o overlay e os atalhos da Game Bar que interceptam teclas e consomem recursos.",
			Do: []Action{
				R(`HKCU\Software\Microsoft\GameBar`, "ShowStartupPanel", "REG_DWORD", "0"),
				R(`HKCU\Software\Microsoft\GameBar`, "UseNexusForGameBarEnabled", "REG_DWORD", "0")}},

		Tweak{ID: "perf-mmcss-games", Cat: catPerf, Risk: "seguro",
			Name: "Prioridade máxima para jogos (MMCSS)",
			Desc: "Ajusta o agendador multimídia: jogos recebem prioridade alta de CPU e GPU.",
			Do: []Action{
				R(mmcss+`\Tasks\Games`, "GPU Priority", "REG_DWORD", "8"),
				R(mmcss+`\Tasks\Games`, "Priority", "REG_DWORD", "6"),
				R(mmcss+`\Tasks\Games`, "Scheduling Category", "REG_SZ", "High"),
				R(mmcss+`\Tasks\Games`, "SFIO Priority", "REG_SZ", "High")}},

		Tweak{ID: "perf-sysresp", Cat: catPerf, Risk: "seguro",
			Name: "Reserva de CPU para multimídia = 0%",
			Desc: "SystemResponsiveness em 0: o jogo pode usar praticamente toda a CPU em vez de reservar 20% para tarefas de fundo.",
			Do:   []Action{R(mmcss, "SystemResponsiveness", "REG_DWORD", "0")}},

		Tweak{ID: "perf-prisep", Cat: catPerf, Risk: "seguro",
			Name: "Priorizar programa em primeiro plano",
			Desc: "Win32PrioritySeparation = 0x26: fatias de CPU curtas e maior boost para a janela ativa, deixando o jogo mais responsivo.",
			Do:   []Action{R(`HKLM\SYSTEM\CurrentControlSet\Control\PriorityControl`, "Win32PrioritySeparation", "REG_DWORD", "38")}},

		Tweak{ID: "perf-powerthrottle", Cat: catPerf, Risk: "seguro",
			Name: "Desativar Power Throttling",
			Desc: "Impede o Windows de reduzir a velocidade de processos que ele considera \"de fundo\" (inclui launchers e anti-cheats).",
			Do:   []Action{R(`HKLM\SYSTEM\CurrentControlSet\Control\Power\PowerThrottling`, "PowerThrottlingOff", "REG_DWORD", "1")}},

		Tweak{ID: "perf-fso", Cat: catPerf, Risk: "moderado",
			Name: "Desativar otimizações de tela cheia",
			Desc: "Força o modo tela cheia exclusivo clássico em vez do modo híbrido; pode reduzir input lag em alguns jogos DX9/DX11.",
			Do: []Action{
				R(gcs, "GameDVR_FSEBehaviorMode", "REG_DWORD", "2"),
				R(gcs, "GameDVR_HonorUserFSEBehaviorMode", "REG_DWORD", "1"),
				R(gcs, "GameDVR_FSEBehavior", "REG_DWORD", "2"),
				R(gcs, "GameDVR_DXGIHonorFSEWindowsCompatible", "REG_DWORD", "1")}},

		Tweak{ID: "perf-windowed", Cat: catPerf, Risk: "seguro",
			Name: "Otimizações para jogos em janela",
			Desc: "Ativa o swap-chain moderno (flip model) em jogos DX10/11 em janela ou sem bordas, com menos latência.",
			Do:   []Action{R(`HKCU\Software\Microsoft\DirectX\UserGpuPreferences`, "DirectXUserGlobalSettings", "REG_SZ", "SwapEffectUpgradeEnable=1;")}},

		Tweak{ID: "perf-faststartup", Cat: catPerf, Risk: "seguro", Reboot: true,
			Name: "Desativar Inicialização Rápida",
			Desc: "Faz o desligamento ser completo, evitando drivers \"presos\" e bugs de desempenho que só somem ao reiniciar.",
			Do:   []Action{R(`HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Power`, "HiberbootEnabled", "REG_DWORD", "0")}},

		Tweak{ID: "perf-hibernate", Cat: catPerf, Risk: "seguro",
			Name: "Desativar hibernação (libera espaço em disco)",
			Desc: "Apaga o hiberfil.sys, que ocupa vários GB (cerca de 40% da RAM) no disco do sistema.",
			Do:   []Action{C(`powercfg -h off`)}, Undo: []Action{C(`powercfg -h on`)}},

		Tweak{ID: "perf-hvci", Cat: catPerf, Risk: "moderado", Reboot: true,
			Name: "Desativar Integridade de Memória (VBS/HVCI)",
			Desc: "O Ryzen 1600 não tem aceleração de hardware para isso, então desligar costuma devolver FPS. Reduz uma camada de segurança.",
			Do: []Action{
				R(`HKLM\SYSTEM\CurrentControlSet\Control\DeviceGuard\Scenarios\HypervisorEnforcedCodeIntegrity`, "Enabled", "REG_DWORD", "0")}},

		Tweak{ID: "perf-sysmain", Cat: catPerf, Risk: "moderado",
			Name: "Desativar SysMain (Superfetch)",
			Desc: "Para de pré-carregar apps na RAM. Bom para SSD; em HD comum o ideal é manter ligado.",
			Do:   []Action{S("SysMain", "disabled")}},

		Tweak{ID: "perf-memcomp", Cat: catPerf, Risk: "moderado", Reboot: true,
			Name: "Desativar compressão de memória",
			Desc: "Com 16 GB de RAM a compressão só gasta CPU. Desligar libera ciclos do processador.",
			Do:   []Action{P(`Disable-MMAgent -MemoryCompression`)}, Undo: []Action{P(`Enable-MMAgent -MemoryCompression`)}},

		Tweak{ID: "perf-bgapps", Cat: catPerf, Risk: "seguro",
			Name: "Bloquear apps em segundo plano",
			Desc: "Apps da Microsoft Store deixam de rodar escondidos, economizando RAM, CPU e disco.",
			Do: []Action{
				R(`HKCU\Software\Microsoft\Windows\CurrentVersion\BackgroundAccessApplications`, "GlobalUserDisabled", "REG_DWORD", "1"),
				R(`HKLM\SOFTWARE\Policies\Microsoft\Windows\AppPrivacy`, "LetAppsRunInBackground", "REG_DWORD", "2")}},

		Tweak{ID: "perf-onedrive-run", Cat: catPerf, Risk: "seguro",
			Name: "Impedir OneDrive de iniciar com o Windows",
			Desc: "Remove o OneDrive da inicialização: boot mais rápido e menos processos consumindo disco e rede.",
			Do:   []Action{D(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "OneDrive")}},

		Tweak{ID: "perf-delivopt", Cat: catPerf, Risk: "seguro",
			Name: "Desativar Otimização de Entrega (P2P do Windows Update)",
			Desc: "O Windows para de usar sua internet para enviar atualizações a outros PCs, evitando lag online.",
			Do:   []Action{R(`HKLM\SOFTWARE\Policies\Microsoft\Windows\DeliveryOptimization`, "DODownloadMode", "REG_DWORD", "0")}},

		Tweak{ID: "perf-procmin", Cat: catPerf, Risk: "seguro",
			Name: "Estado mínimo do processador em 100%",
			Desc: "A CPU não baixa o clock no desktop, eliminando o atraso de \"acordar\" ao abrir ou iniciar um jogo.",
			Do:   []Action{C(pw("SUB_PROCESSOR", "PROCTHROTTLEMIN", "100"))},
			Undo: []Action{C(pw("SUB_PROCESSOR", "PROCTHROTTLEMIN", "5"))}},

		Tweak{ID: "perf-pcie", Cat: catPerf, Risk: "seguro",
			Name: "Desativar economia de energia do PCI Express",
			Desc: "Mantém o link da RX 580 sempre em velocidade máxima, evitando quedas rápidas de desempenho.",
			Do:   []Action{C(pw("SUB_PCIEXPRESS", "ASPM", "0"))},
			Undo: []Action{C(pw("SUB_PCIEXPRESS", "ASPM", "1"))}},

		// ===================== LATÊNCIA E ENTRADA =====================
		Tweak{ID: "in-mouseaccel", Cat: catInput, Risk: "seguro",
			Name: "Desativar aceleração do mouse",
			Desc: "Tira o \"Aprimorar precisão do ponteiro\": o movimento fica 1:1 e a mira fica consistente em FPS.",
			Do: []Action{
				R(`HKCU\Control Panel\Mouse`, "MouseSpeed", "REG_SZ", "0"),
				R(`HKCU\Control Panel\Mouse`, "MouseThreshold1", "REG_SZ", "0"),
				R(`HKCU\Control Panel\Mouse`, "MouseThreshold2", "REG_SZ", "0")}},

		Tweak{ID: "in-mousesens", Cat: catInput, Risk: "seguro",
			Name: "Sensibilidade do ponteiro no padrão 6/11",
			Desc: "Garante a velocidade neutra do Windows (sem multiplicadores), a base certa para ajustar só pela DPI do mouse.",
			Do:   []Action{R(`HKCU\Control Panel\Mouse`, "MouseSensitivity", "REG_SZ", "10")}},

		Tweak{ID: "in-sticky", Cat: catInput, Risk: "seguro",
			Name: "Desativar atalhos de Teclas de Aderência",
			Desc: "Acaba com a janela que aparece ao apertar Shift 5x durante o jogo e te joga para a área de trabalho.",
			Do: []Action{
				R(`HKCU\Control Panel\Accessibility\StickyKeys`, "Flags", "REG_SZ", "506"),
				R(`HKCU\Control Panel\Accessibility\Keyboard Response`, "Flags", "REG_SZ", "122"),
				R(`HKCU\Control Panel\Accessibility\ToggleKeys`, "Flags", "REG_SZ", "58")}},

		Tweak{ID: "in-kbfast", Cat: catInput, Risk: "seguro",
			Name: "Teclado com repetição mais rápida",
			Desc: "Atraso mínimo e velocidade máxima de repetição de teclas, deixando a resposta do teclado mais ágil.",
			Do: []Action{
				R(`HKCU\Control Panel\Keyboard`, "KeyboardDelay", "REG_SZ", "0"),
				R(`HKCU\Control Panel\Keyboard`, "KeyboardSpeed", "REG_SZ", "31")}},

		Tweak{ID: "in-timer", Cat: catInput, Risk: "seguro", Reboot: true,
			Name: "Respeitar resolução de timer solicitada por jogos",
			Desc: "Permite que cada jogo peça o timer de 0,5 ms ao Windows, resultando em frametime mais estável.",
			Do:   []Action{R(`HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\kernel`, "GlobalTimerResolutionRequests", "REG_DWORD", "1")}},

		Tweak{ID: "in-dyntick", Cat: catInput, Risk: "moderado", Reboot: true,
			Name: "Desativar Dynamic Tick",
			Desc: "O timer do sistema passa a ser constante, reduzindo variação de latência. Aumenta um pouco o consumo em repouso.",
			Do:   []Action{C(`bcdedit /set disabledynamictick yes`)}, Undo: []Action{C(`bcdedit /deletevalue disabledynamictick & exit /b 0`)}},

		Tweak{ID: "in-hpet", Cat: catInput, Risk: "seguro", Reboot: true,
			Name: "Não forçar o HPET como relógio",
			Desc: "Remove a opção useplatformclock caso esteja ativa. Forçar HPET aumenta a latência em CPUs Ryzen.",
			Do:   []Action{C(`bcdedit /deletevalue useplatformclock >nul 2>&1 & exit /b 0`)}},

		Tweak{ID: "in-usbsuspend", Cat: catInput, Risk: "seguro",
			Name: "Desativar suspensão seletiva de USB",
			Desc: "Evita que mouse/teclado \"durmam\" e demorem para responder ou desconectem no meio da partida.",
			Do:   []Action{C(pw("2a737441-1930-4402-8d77-b2bebba308a3", "48e6b7a6-50f5-4782-a5d4-53bb8f07e226", "0"))},
			Undo: []Action{C(pw("2a737441-1930-4402-8d77-b2bebba308a3", "48e6b7a6-50f5-4782-a5d4-53bb8f07e226", "1"))}},

		Tweak{ID: "in-mpo", Cat: catInput, Risk: "moderado", Reboot: true,
			Name: "Desativar Multiplane Overlay (MPO)",
			Desc: "Resolve engasgos, flickering e stutters em placas AMD em alguns jogos e monitores com alta taxa de atualização.",
			Do:   []Action{R(`HKLM\SOFTWARE\Microsoft\Windows\Dwm`, "OverlayTestMode", "REG_DWORD", "5")}},

		// ===================== CPU RYZEN =====================
		Tweak{ID: "cpu-corepark", Cat: catCPU, Risk: "seguro",
			Name: "Desativar estacionamento de núcleos (Core Parking)",
			Desc: "Mantém os 6 núcleos / 12 threads do Ryzen 5 1600 sempre disponíveis, evitando picos de latência.",
			Do:   []Action{C(pw("SUB_PROCESSOR", "CPMINCORES", "100"))},
			Undo: []Action{C(pw("SUB_PROCESSOR", "CPMINCORES", "10"))}},

		Tweak{ID: "cpu-boost", Cat: catCPU, Risk: "seguro",
			Name: "Boost de CPU agressivo",
			Desc: "Modo de boost \"Aggressive\": o Ryzen sobe ao clock máximo mais rápido quando o jogo pede desempenho.",
			Do:   []Action{C(pw("SUB_PROCESSOR", "PERFBOOSTMODE", "2"))},
			Undo: []Action{C(pw("SUB_PROCESSOR", "PERFBOOSTMODE", "1"))}},

		// ===================== GPU RX 580 =====================
		Tweak{ID: "gpu-ulps", Cat: catGPU, Risk: "seguro", Reboot: true,
			Name: "Desativar ULPS da Radeon",
			Desc: "Desliga o Ultra Low Power State do driver AMD, que pode causar travadas e telas pretas na RX 580.",
			Do:   []Action{P(`$b='HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}'; Get-ChildItem $b -ErrorAction SilentlyContinue | Where-Object { $_.PSChildName -match '^\d{4}$' } | ForEach-Object { $d=(Get-ItemProperty $_.PSPath).DriverDesc; if($d -match 'Radeon|AMD'){ Set-ItemProperty $_.PSPath -Name EnableUlps -Value 0 -Type DWord -Force; Set-ItemProperty $_.PSPath -Name EnableUlps_NA -Value '0' -Type String -Force } }`)},
			Undo: []Action{P(`$b='HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}'; Get-ChildItem $b -ErrorAction SilentlyContinue | Where-Object { $_.PSChildName -match '^\d{4}$' } | ForEach-Object { $d=(Get-ItemProperty $_.PSPath).DriverDesc; if($d -match 'Radeon|AMD'){ Set-ItemProperty $_.PSPath -Name EnableUlps -Value 1 -Type DWord -Force } }`)}},

		Tweak{ID: "gpu-tdr", Cat: catGPU, Risk: "seguro", Reboot: true,
			Name: "Aumentar tempo limite do driver de vídeo (TDR)",
			Desc: "Dá mais tempo à RX 580 antes do Windows reiniciar o driver, evitando o erro \"driver parou de responder\".",
			Do: []Action{
				R(`HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`, "TdrDelay", "REG_DWORD", "10"),
				R(`HKLM\SYSTEM\CurrentControlSet\Control\GraphicsDrivers`, "TdrDdiDelay", "REG_DWORD", "10")}},
	)
}
