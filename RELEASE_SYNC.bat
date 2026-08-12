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
"!ISCC!" installer\siegeiq-sync.iss
if not exist "installer\dist\SiegeIQSync-Setup.exe" (
  echo.
  echo   [STOP] Inno Setup ran but produced no installer. Whatever it
  echo          printed just above this line is the reason - send it over.
  echo.
  pause
  exit /b 1
)
echo   Built installer\dist\SiegeIQSync-Setup.exe

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
