Put ffmpeg.exe in this folder.

WHY
The SiegeIQ Sync recorder uses FFmpeg to capture and encode gameplay. It runs it
as a separate program - FFmpeg is never linked into SiegeIQSync.exe - so all the
tricky work of talking to NVIDIA, AMD and Intel hardware encoders is handled by
software that already does it well on every kind of PC.

WHICH BUILD
Get a Windows build that includes the "ddagrab" filter (FFmpeg 6.0 or newer).
The gyan.dev "full" or "essentials" builds and the BtbN builds both include it.
Without ddagrab the recorder still works, but falls back to a slower
window-capture path that uses more CPU.

WHAT TO DO
1. Download a Windows FFmpeg build (a .zip or .7z).
2. Open it and find bin\ffmpeg.exe.
3. Copy ONLY ffmpeg.exe into this folder, next to this text file.
4. Also copy the build's LICENSE file in here and rename it
   FFMPEG-LICENSE.txt - the installer ships it alongside, which is what the
   attribution requirement asks for.
5. Run build.bat as usual. The installer picks both files up automatically.

If this folder is empty the build still succeeds and the installer is still
valid. The recorder simply reports that it cannot find a capture engine, and
replay syncing is completely unaffected.
