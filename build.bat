@echo off
REM Build SiegeIQ Sync (needs Go installed: https://go.dev/dl/)
cd /d "%~dp0"
go build -ldflags="-s -w" -o SiegeIQSync.exe
if %errorlevel%==0 (
  echo Built SiegeIQSync.exe
) else (
  echo Build failed - is Go installed and on PATH?
)
pause
