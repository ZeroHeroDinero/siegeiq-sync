@echo off
REM Build SiegeIQ Sync (needs Go: https://go.dev/dl/).
REM Also builds the installer if Inno Setup 6 is installed.
cd /d "%~dp0"

REM One-time (and after any go.mod change): fetch deps and refresh go.sum.
go mod tidy
if not %errorlevel%==0 (
  echo go mod tidy failed - check your internet connection and that Go is on PATH.
  pause
  exit /b 1
)

REM Embed the branded .exe icon + version info (Explorer / Task Manager / Alt-Tab).
where go-winres >nul 2>nul
if %errorlevel%==0 (
  go-winres make
) else (
  echo [skipped] go-winres not installed - SiegeIQSync.exe will build with the default Go icon.
  echo           Run: go install github.com/tc-hib/go-winres@latest
  echo           Then re-run this script once to embed the branded icon.
)

REM -H=windowsgui hides the console window - SiegeIQ Sync lives in the system tray only.
go build -ldflags="-s -w -H=windowsgui" -o SiegeIQSync.exe
if not %errorlevel%==0 (
  echo Build failed - is Go installed and on PATH?
  pause
  exit /b 1
)
echo Built SiegeIQSync.exe

REM Build the installer too, if Inno Setup 6 (ISCC.exe) is available.
set "ISCC=%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe"
if exist "%ISCC%" (
  "%ISCC%" installer\siegeiq-sync.iss && echo Built installer\dist\SiegeIQSync-Setup.exe || echo Installer build failed.
) else (
  echo [skipped] Inno Setup 6 not found - get it from https://jrsoftware.org/isdl.php
  echo           to also produce installer\dist\SiegeIQSync-Setup.exe
)
pause
