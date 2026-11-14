@echo off
REM Compila o GamerBoost.exe no seu PC (precisa do Go instalado: https://go.dev/dl)
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags="-s -w -H=windowsgui" -o GamerBoost.exe .
if errorlevel 1 (echo Erro ao compilar) else (echo Pronto: GamerBoost.exe)
pause
