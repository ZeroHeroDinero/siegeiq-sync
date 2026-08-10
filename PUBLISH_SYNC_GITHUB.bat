@echo off
REM ===================================================================
REM  Publish the SiegeIQ Sync SOURCE to GitHub.
REM
REM  Safe to run more than once. The first run sets the repository up,
REM  every run after that just pushes whatever has changed.
REM
REM  This pushes SOURCE ONLY. It does not create a release and it does
REM  not change what siegeiq.gg offers for download. Cutting the release
REM  is the separate RELEASE_SYNC.bat.
REM ===================================================================
cd /d "%~dp0"
setlocal

echo.
echo   SiegeIQ Sync - publishing source to GitHub
echo   ------------------------------------------
echo.

where git >nul 2>nul
if not %errorlevel%==0 (
  echo   [STOP] Git is not installed, or is not on your PATH.
  echo          Get it from https://git-scm.com/download/win then run this again.
  echo.
  pause
  exit /b 1
)

if not exist ".git" (
  echo   First run. Setting the repository up...
  git init -b main
  git remote add origin https://github.com/ZeroHeroDinero/siegeiq-sync.git
  echo   Fetching what is already on GitHub so nothing there is lost...
  git fetch origin main
  if %errorlevel%==0 (
    git reset --soft origin/main
  ) else (
    echo   Nothing on GitHub yet, or it could not be reached. Carrying on.
  )
) else (
  echo   Repository already set up.
)

echo.
echo   Files that will be published:
git add -A
git status --short
echo.

REM ---- Safety check: nothing enormous, nothing secret. ----------------
for /f %%A in ('git diff --cached --name-only ^| find /c /v ""') do set COUNT=%%A
echo   %COUNT% file(s) staged.
if exist "ffmpeg\ffmpeg.exe" (
  git check-ignore -q "ffmpeg/ffmpeg.exe"
  if not %errorlevel%==0 (
    echo.
    echo   [STOP] ffmpeg.exe is about to be committed. It is 99 MB and GitHub
    echo          will reject it. The .gitignore in this folder is missing or wrong.
    echo.
    pause
    exit /b 1
  )
)

echo.
set /p MSG="  Describe this change (or press Enter for the default): "
if "%MSG%"=="" set MSG=Recorder: screen capture, clip keep rules, app window

git commit -m "%MSG%"
if not %errorlevel%==0 (
  echo   Nothing to commit. Everything on GitHub is already up to date.
  echo.
  pause
  exit /b 0
)

echo.
echo   Pushing to https://github.com/ZeroHeroDinero/siegeiq-sync ...
git push -u origin main
if not %errorlevel%==0 (
  echo.
  echo   [STOP] The push failed. The usual cause is that Git has never been
  echo          signed in on this PC. Run this and follow the browser prompt:
  echo.
  echo             git credential-manager github login
  echo.
  echo          Then run this file again. Nothing was lost.
  echo.
  pause
  exit /b 1
)

echo.
echo   Done. The source is now public at:
echo     https://github.com/ZeroHeroDinero/siegeiq-sync
echo.
echo   The download button on siegeiq.gg has NOT changed. It still serves the
echo   old release until you run RELEASE_SYNC.bat.
echo.
pause
