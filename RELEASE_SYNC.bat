@echo off
REM ===================================================================
REM  Cut a SiegeIQ Sync release and publish it to GitHub.
REM
REM  READ THIS FIRST. The download button on siegeiq.gg points at
REM  "releases/latest". The moment this finishes, that button starts
REM  serving THIS build to the public.
REM
REM  You do not need to open Inno Setup yourself. This script drives it.
REM  All Inno Setup does is take SiegeIQSync.exe and wrap it into
REM  SiegeIQSync-Setup.exe, which is the file people actually download.
REM ===================================================================
cd /d "%~dp0"
setlocal enabledelayedexpansion

for /f "tokens=2 delims==" %%V in ('findstr /c:"const version" config.go') do set RAW=%%V
set VER=%RAW:"=%
set VER=%VER: =%

echo.
echo   SiegeIQ Sync - release v%VER%
echo   -----------------------------------

REM ---- DRIFT CHECK. Added 2026-08-13, and it is here because of a real cost.
REM
REM      config.go was bumped to 1.4.3, 1.4.4, 1.4.5, 1.4.6, 1.4.7 and 1.4.8
REM      and NONE of them were ever published. siegeiq.gg kept serving 1.4.2
REM      the whole time, because the /sync page reads the newest GitHub
REM      release and nothing else. Bumping the number in the file feels like
REM      releasing and is not, and there was no signal anywhere that the two
REM      had drifted six versions apart.
REM
REM      A user eventually reported a bug that had been fixed months earlier
REM      in a build nobody could download. This is the check that would have
REM      caught it the first time.
for /f "delims=" %%L in ('powershell -NoProfile -Command ^
  "try{(Invoke-RestMethod -TimeoutSec 8 'https://siegeiq-backend-production.up.railway.app/sync/latest').version}catch{'?'}"') do set "LIVEVER=%%L"
if defined LIVEVER (
  if "!LIVEVER!"=="?" (
    echo   Live version: could not check ^(offline?^). Continuing.
  ) else if "!LIVEVER!"=="%VER%" (
    echo   Live version: v!LIVEVER! - already published. This will re-release it.
  ) else (
    echo   Live version: v!LIVEVER!   ^->   publishing v%VER%
  )
)


REM ---- Find Inno Setup, wherever it decided to install itself --------
REM
REM This used to check ONE path and give up. Inno Setup installs to at
REM least four different places depending on whether it was the 32-bit
REM or 64-bit build, whether it was installed for everyone or just you,
REM and whether it came from winget. Checking one of them and declaring
REM it missing is how somebody who HAS it installed gets told they do not.
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

REM ---- Last resort: actually go and look for it ---------------------
REM
REM The four known paths and PATH all missed on a machine that genuinely
REM had Inno Setup installed. Guessing a fifth path would just move the
REM problem, so this searches the folders software actually installs into
REM and finds ISCC.exe wherever it ended up. It takes a few seconds and
REM only runs when everything cheaper has already failed.
if not defined ISCC (
  echo   Searching this PC for Inno Setup, this takes a few seconds...
  for %%R in (
    "%ProgramFiles%"
    "%ProgramFiles(x86)%"
    "%LOCALAPPDATA%\Programs"
    "%LOCALAPPDATA%\Microsoft\WinGet"
    "%ProgramData%"
  ) do if not defined ISCC if exist %%R (
    for /f "delims=" %%P in ('where /R %%R ISCC.exe 2^>nul') do if not defined ISCC set "ISCC=%%P"
  )
)

if defined ISCC (
  echo   Inno Setup found: !ISCC!
) else (
  echo.
  echo   [STOP] Inno Setup could not be found anywhere on this PC.
echo.
echo          If you have never installed it, install it in one line:
echo             winget install -e --id JRSoftware.InnoSetup
echo          then run this script again.
  echo.
  echo          Find it yourself in one line: open the Start menu, type
  echo          "Inno Setup", right-click it, choose "Open file location",
  echo          then right-click the shortcut there and choose
  echo          "Open file location" again. Copy that folder path and
  echo          send it over, and this script will be taught about it.
  echo.
  pause
  exit /b 1
)

REM ---- The recorder gate. Added 2026-08-13 after a real user got "No capture
REM      engine" on a published build.
REM
REM      ffmpeg.exe is ~100 MB, is gitignored (over GitHub's file limit) and is
REM      pulled into the installer by a line flagged skipifsourcedoesntexist. So
REM      a build from a fresh clone, or from any folder where that file has not
REM      been placed by hand, produces a COMPLETE, VALID installer with no
REM      recorder in it, and says nothing at all.
REM
REM      That is exactly what shipped. This is the check that makes it loud. It
REM      is a warning and not a hard stop, because a sync-only release is a
REM      legitimate thing to publish - but it can no longer happen by accident.
set "FFOK=1"
REM %CD% is the siegeiq-sync folder, so this is a fully resolved absolute path with no
REM ".." left in it for the compiler to interpret its own way.
set "ISCCFLAGS=/DFFmpegDir=%CD%\ffmpeg"
if not exist "ffmpeg\ffmpeg.exe" set "FFOK="
if not defined FFOK (
  echo.
  echo   ============================================================
  echo   [WARNING] ffmpeg\ffmpeg.exe is MISSING.
  echo   ============================================================
  echo   This release will install with NO SCREEN RECORDER. Replay
  echo   syncing will work; every user who tries to record will see
  echo   "No capture engine" and cannot fix it themselves.
  echo.
  echo   To include it: read ffmpeg\README-PUT-FFMPEG-HERE.txt, drop
  echo   bin\ffmpeg.exe into the ffmpeg folder, and re-run this.
  echo.
  set /p FFGO="  Publish a sync-only build anyway? Type NORECORDER: "
  if /i not "!FFGO!"=="NORECORDER" (
    echo   Cancelled. Nothing was published.
    pause
    exit /b 0
  )
  REM Tell the installer script this is deliberate. Without it the compile now
  REM STOPS on a missing ffmpeg instead of quietly producing a crippled build.
  set "ISCCFLAGS=/DNoRecorder /DFFmpegDir=!CD!\ffmpeg"
) else (
  echo   Recorder: ffmpeg.exe present, will be bundled.
)

echo.
echo   This publishes v%VER%. siegeiq.gg will start offering it immediately.
echo.
set /p GO="  Type YES to continue: "
if /i not "%GO%"=="YES" (
  echo   Cancelled. Nothing was published.
  pause
  exit /b 0
)

echo.
echo   [1/5] Building the app...
REM SIQ_RELEASE tells build.bat that a release is driving, so it skips its
REM own installer step and its "press any key" prompt.
set "SIQ_RELEASE=1"
call build.bat
if not exist "SiegeIQSync.exe" (
  echo   [STOP] SiegeIQSync.exe was not produced. Fix the build first.
  pause
  exit /b 1
)

echo.
echo   [2/5] Wrapping it into an installer...
REM Keep the compiler's own words. When the installer comes out the wrong size, this
REM file is the only thing that says why, and it is far quicker than another guess.
"!ISCC!" !ISCCFLAGS! installer\siegeiq-sync.iss > installer\iscc_log.txt 2>&1
type installer\iscc_log.txt
echo   ^(full compiler output saved to installer\iscc_log.txt^)
if not exist "installer\dist\SiegeIQSync-Setup.exe" (
  echo.
  echo   [STOP] Inno Setup ran but produced no installer. Whatever it
  echo          printed just above this line is the reason - send it over.
  echo.
  pause
  exit /b 1
)
echo   Built installer\dist\SiegeIQSync-Setup.exe

REM ---- Did the recorder actually get IN? Added 2026-08-13.
REM
REM      The gate earlier checks that ffmpeg.exe EXISTS. This checks that it
REM      ended up inside the installer, which is a different question and the
REM      one that actually bit a user. A build that silently drops a 100 MB
REM      payload produces an installer roughly a third of the size, so the
REM      number is not subtle - but only if somebody looks at it. This makes
REM      it impossible not to.
REM ---- ASK THE COMPILER, DO NOT GUESS FROM THE SIZE. Corrected 2026-08-13.
REM
REM      The first version of this check used a size threshold and it was WRONG,
REM      and it blocked three consecutive releases for no reason.
REM
REM      The reasoning was: ffmpeg.exe is 98 MB, so an installer containing it
REM      must be far bigger than one without. It is not. LZMA2/max with solid
REM      compression takes that 98 MB down to about 22 MB, so the real installer
REM      WITH the recorder is 30.7 MB and one without would be about 9 MB. The
REM      threshold was set at 36 MB on an estimate that was off by nearly 3x.
REM
REM      ISCC prints one line per file it packs. That is direct evidence and it
REM      cannot be off by a compression ratio, so that is what gets checked.
for %%A in ("installer\dist\SiegeIQSync-Setup.exe") do set /a SETUPMB=%%~zA/1048576
findstr /I /C:"Compressing" "installer\iscc_log.txt" 2>nul | findstr /I /C:"ffmpeg.exe" >nul
if not errorlevel 1 (
  set "PACKED=1"
) else (
  set "PACKED="
)
if defined FFOK (
  if not defined PACKED (
    echo.
    echo   ============================================================
    echo   [STOP] The compiler did not pack ffmpeg.exe.
    echo   ============================================================
    echo   ffmpeg.exe is on disk but installer\iscc_log.txt has no
    echo   "Compressing: ...ffmpeg.exe" line. Read that file - it names
    echo   the exact path it looked at, near the top.
    echo.
    pause
    exit /b 1
  )
  echo   Recorder CONFIRMED in the installer by the compiler. ^(!SETUPMB! MB^)
) else (
  echo   Installer size: !SETUPMB! MB  ^(sync-only, no recorder^)
)

REM A floor well below any real build, purely to catch a truncated or failed output.
if !SETUPMB! LSS 5 (
  echo   [STOP] Installer is only !SETUPMB! MB. Something went wrong.
  pause
  exit /b 1
)

echo.
echo   [3/5] Writing the checksums the /sync page verifies against...
powershell -NoProfile -Command ^
  "$f=@('SiegeIQSync.exe','installer\dist\SiegeIQSync-Setup.exe');" ^
  "$f | ForEach-Object { $h=(Get-FileHash $_ -Algorithm SHA256).Hash.ToLower(); '{0}  {1}' -f $h,(Split-Path $_ -Leaf) } | Set-Content -Encoding ascii SHA256SUMS.txt"
type SHA256SUMS.txt

echo.
echo   [4/5] Pushing the source that produced this build...
call PUBLISH_SYNC_GITHUB.bat

echo.
echo   [5/5] Creating the GitHub release...
where gh >nul 2>nul
if %errorlevel%==0 (
  gh release create v%VER% ^
    "SiegeIQSync.exe" ^
    "installer\dist\SiegeIQSync-Setup.exe" ^
    "SHA256SUMS.txt" ^
    --repo ZeroHeroDinero/siegeiq-sync ^
    --title "SiegeIQ Sync v%VER%" ^
    --generate-notes
  if !errorlevel!==0 (
    echo.
    echo   Published. siegeiq.gg now serves v%VER%.
    echo   The version shown on the page can lag by up to ten minutes.
  ) else (
    echo.
    echo   The release step failed. Everything else succeeded, so finish
    echo   it by hand using the instructions below.
    goto MANUAL
  )
) else (
  goto MANUAL
)
goto DONE

:MANUAL
echo.
echo   The GitHub CLI is not installed, so the release is made in the
echo   browser. About a minute:
echo.
echo     1. Open https://github.com/ZeroHeroDinero/siegeiq-sync/releases/new
echo     2. Tag:   v%VER%          (choose "Create new tag")
echo     3. Title: SiegeIQ Sync v%VER%
echo     4. Drag these three files onto the "Attach binaries" box:
echo          %CD%\SiegeIQSync.exe
echo          %CD%\installer\dist\SiegeIQSync-Setup.exe
echo          %CD%\SHA256SUMS.txt
echo     5. Click "Publish release".
echo.
echo   To skip this next time: winget install --id GitHub.cli
echo.

:DONE
echo.
pause
