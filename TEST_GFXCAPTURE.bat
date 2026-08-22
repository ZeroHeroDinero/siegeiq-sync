@echo off
cd /d "%~dp0"
echo(
echo  SiegeIQ Sync - rehearsing the new capture chain
echo  ----------------------------------------------
echo(
echo  This grabs two frames of your main screen and throws them away.
echo  Nothing is saved and nothing is uploaded.
echo(
echo  Test 1 of 2, frames arriving on the graphics card...
ffmpeg\ffmpeg.exe -hide_banner -loglevel error -filter_complex "gfxcapture=monitor_idx=0,hwdownload,format=bgra[v]" -map [v] -frames:v 2 -f null - 2>gfx_test_a.txt
if errorlevel 1 (echo     no) else (echo     YES)
echo(
echo  Test 2 of 2, frames arriving in normal memory...
ffmpeg\ffmpeg.exe -hide_banner -loglevel error -filter_complex "gfxcapture=monitor_idx=0,format=bgra[v]" -map [v] -frames:v 2 -f null - 2>gfx_test_b.txt
if errorlevel 1 (echo     no) else (echo     YES)
echo(
echo  ----------------------------------------------
echo  One YES is all that is needed. Two nos means do
echo  not release yet, tell Claude.
echo(
pause
