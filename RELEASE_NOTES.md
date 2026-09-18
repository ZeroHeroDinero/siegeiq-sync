## What this is

SiegeIQ Sync reads the replay files Siege already writes for every match you play and
sends them to your SiegeIQ account, so your Verified Stats fill in on their own.

It does not record your screen. It does not go near the game.

## What changed in this version

**The download is small now.** Around 5 MB instead of 31 MB. The screen recorder used to
be baked into every install whether you wanted it or not, and it was most of the download.
It is no longer included.

**Recording is still there if you want it.** Turn it on inside the app and Sync fetches
that part on its own, once, and checks it before using it. If you never turn it on, nothing
extra is ever downloaded.

**Everything else works the same.** Same pairing code, same folder, same uploads, same
Verified Stats. If you already have Sync installed it updates itself.

## Files

- `SiegeIQSync-Setup.exe` is the one to download if you are installing for the first time.
- `SiegeIQSync.exe` is the portable build. The app uses this one to update itself.
- `SHA256SUMS.txt` lets you check either file is genuine.

Windows may warn you the first time you run it because the build is not code-signed yet.
Click More info, then Run anyway.
