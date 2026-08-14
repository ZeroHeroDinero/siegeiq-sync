@echo off
REM Build SiegeIQ Sync (needs Go: https://go.dev/dl/).
REM Also builds the installer if Inno Setup 6 is installed.
cd /d "%~dp0"

REM setlocal is not tidiness, it is a bug fix. RELEASE_SYNC.bat CALLS this
REM script, and without setlocal every variable set here overwrites the
REM caller's. That is exactly what happened: this script cleared ISCC while
REM looking for Inno Setup, failed to find it, and handed an EMPTY ISCC back
REM to a release script that had already found it. Step 2 then tried to run a
REM program called "" and stopped the release.
setlocal

REM One-time (and after any go.mod change): fetch deps and refresh go.sum.
go mod tidy
if not %errorlevel%==0 (
  echo go mod tidy failed - check your internet connection and that Go is on PATH.
  pause
  exit /b 1
)

REM Embed the branded .exe icon + version info (Explorer / Task Manager / Alt-Tab).
REM "go install" puts tools in %USERPROFILE%\go\bin, and the Go installer does
REM NOT add that folder to PATH. So "where go-winres" says no straight after a
REM successful install, and the advice above it sends you round in a circle.
set "WINRES="
where go-winres >nul 2>nul && set "WINRES=go-winres"
if not defined WINRES if exist "%USERPROFILE%\go\bin\go-winres.exe" set "WINRES=%USERPROFILE%\go\bin\go-winres.exe"
if defined WINRES (
  "%WINRES%" make
  echo Embedded the SiegeIQ icon and version info.
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

REM During a release, RELEASE_SYNC.bat builds the installer itself with its
REM own, better search. Doing it twice is wasted work and a confusing
REM "[skipped] Inno Setup not found" in the middle of a successful release.
if defined SIQ_RELEASE goto SKIPINSTALLER

REM Build the installer too, if Inno Setup (ISCC.exe) is available.
REM Inno Setup installs to at least four different places depending on the
REM installer used. Checking one of them is how somebody who HAS it installed
REM gets told they do not. Same search RELEASE_SYNC.bat uses.
REM A dev build with no ffmpeg is fine and normal - it is not going to players.
REM The installer script now STOPS on a missing recorder, so say out loud that
REM this one is deliberate. RELEASE_SYNC.bat sets this only when you type
REM NORECORDER, which is the difference between a dev build and a bad release.
set "ISCCFLAGS=/DFFmpegDir=%CD%\ffmpeg"
if not exist "ffmpeg\ffmpeg.exe" (
  echo [note] ffmpeg\ffmpeg.exe not present - building a sync-only installer.
  set "ISCCFLAGS=/DNoRecorder /DFFmpegDir=%CD%\ffmpeg"
)

set "ISCC="
for %%P in (
  "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe"
  "%ProgramFiles%\Inno Setup 6\ISCC.exe"
  "%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe"
  "%ProgramFiles%\Inno Setup 7\ISCC.exe"
  "%ProgramFiles(x86)%\Inno Setup 7\ISCC.exe"
  "%ProgramFiles(x86)%\Inno Setup 5\ISCC.exe"
) do if not defined ISCC if exist %%P set "ISCC=%%~P"
if not defined ISCC (
  for /f "delims=" %%P in ('where ISCC.exe 2^>nul') do if not defined ISCC set "ISCC=%%P"
)
if defined ISCC (
  "%ISCC%" %ISCCFLAGS% installer\siegeiq-sync.iss > installer\iscc_log.txt 2>&1 && echo Built installer\dist\SiegeIQSync-Setup.exe || echo Installer build failed - see installer\iscc_log.txt
  type installer\iscc_log.txt
) else (
  echo [skipped] Inno Setup not found - get it from https://jrsoftware.org/isdl.php
  echo           to also produce installer\dist\SiegeIQSync-Setup.exe
)

:SKIPINSTALLER
REM No "press any key" in the middle of a release. Only when a person ran
REM this script directly and is waiting to read the result.
if not defined SIQ_RELEASE pause
