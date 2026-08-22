@echo off
cd /d "%~dp0"
echo(
echo  SiegeIQ Sync - does this ffmpeg have the new capture source?
echo  -----------------------------------------------------------
echo(
if not exist "ffmpeg\ffmpeg.exe" goto nofile
ffmpeg\ffmpeg.exe -hide_banner -version 2>&1 | findstr /b /c:"ffmpeg version"
echo(
ffmpeg\ffmpeg.exe -hide_banner -filters 2>&1 | findstr /i /c:"gfxcapture" >nul
if errorlevel 1 goto missing
echo  RESULT: YES. This ffmpeg has gfxcapture.
echo(
echo  Go ahead and run RELEASE_SYNC.bat.
goto details
:missing
echo  RESULT: NO. This ffmpeg has no gfxcapture source.
echo(
echo  Publishing 1.5.9 is still safe. It just will not change
echo  anything until a newer ffmpeg is bundled.
goto details
:details
echo(
ffmpeg\ffmpeg.exe -hide_banner -h filter=gfxcapture > gfxcapture_check.txt 2>&1
echo  Full details saved next to this file as gfxcapture_check.txt
goto end
:nofile
echo  ffmpeg.exe was NOT found in the ffmpeg folder.
echo  Nothing else can be checked. Tell Claude this.
:end
echo(
echo  -----------------------------------------------------------
echo  This window stays open until you press a key.
pause
