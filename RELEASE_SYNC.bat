@echo off
REM ===================================================================
REM  Cut a SiegeIQ Sync release and publish it to GitHub.
REM
REM  READ THIS FIRST. The download button on siegeiq.gg points at
REM  "releases/latest". The moment this finishes, that button starts
REM  serving THIS build to the public. Do not run it until the /sync
REM  page copy has been corrected and deployed.
REM ===================================================================
cd /d "%~dp0"
setlocal enabledelayedexpansion

for /f "tokens=2 delims==" %%V in ('findstr /c:"const version" config.go') do set RAW=%%V
set VER=%RAW:"=%
set VER=%VER: =%

echo.
echo   SiegeIQ Sync - release v%VER%
echo   -----------------------------------
echo.
echo   This will publish v%VER% to GitHub and siegeiq.gg will begin
echo   offering it for download immediately.
echo.
set /p GO="  Type YES to continue: "
if /i not "%GO%"=="YES" (
  echo   Cancelled. Nothing was published.
  pause
  exit /b 0
)

echo.
echo   [1/4] Building the app and the installer...
call build.bat
if not exist "SiegeIQSync.exe" (
  echo   [STOP] SiegeIQSync.exe was not produced. Fix the build first.
  pause
  exit /b 1
)
if not exist "installer\dist\SiegeIQSync-Setup.exe" (
  echo.
  echo   [STOP] The installer was not produced. Inno Setup 6 is missing.
  echo          Install it from https://jrsoftware.org/isdl.php then run this again.
  echo.
  pause
  exit /b 1
)

echo.
echo   [2/4] Writing the checksums the /sync page verifies against...
powershell -NoProfile -Command ^
  "$f=@('SiegeIQSync.exe','installer\dist\SiegeIQSync-Setup.exe');" ^
  "$f | ForEach-Object { $h=(Get-FileHash $_ -Algorithm SHA256).Hash.ToLower(); '{0}  {1}' -f $h,(Split-Path $_ -Leaf) } | Set-Content -Encoding ascii SHA256SUMS.txt"
type SHA256SUMS.txt

echo.
echo   [3/4] Pushing the source that produced this build...
call PUBLISH_SYNC_GITHUB.bat

echo.
echo   [4/4] Creating the GitHub release...
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
  ) else (
    echo.
    echo   The release step failed. Everything else succeeded, so you can
    echo   finish it by hand using the instructions below.
    goto MANUAL
  )
) else (
  goto MANUAL
)
goto DONE

:MANUAL
echo.
echo   The GitHub CLI is not installed, so the release has to be made in
echo   the browser. It takes about a minute:
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
