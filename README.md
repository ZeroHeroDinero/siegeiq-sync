# SiegeIQ Sync

The official desktop companion for [SiegeIQ](https://siegeiq.gg). A small Windows app that does two
things while you play.

It watches your Rainbow Six Siege replay folder and uploads each new match to your SiegeIQ account,
so your Verified Stats update on their own. And it records your screen into a rolling buffer, so the
moment worth keeping is already saved by the time you think to save it.

Both halves are optional and independent of each other. Switch the recorder off and it behaves
exactly as it always has. Switch syncing off and it still records. Play, tab out, done.

Lives quietly in your system tray. Windows 10 / 11.

## What it does — and what it never does

**It never touches the game.** SiegeIQ Sync does not read Siege's memory, does not inject anything
into the game, and does not touch its network traffic. It reads files off your disk, and it captures
your screen from the outside in exactly the way OBS or ShadowPlay do. That is why it is irrelevant
to BattlEye, and it is a promise you can check by reading the code below rather than taking our word
for it.

- **Watches exactly one folder:** `Documents\My Games\Rainbow Six - Siege\<id>\MatchReplay`
- **Uploads only `.rec` files.** Nothing else on your disk is read or sent.
- **Records only while Siege is running,** and only the Siege window. Recording stops when you close
  the game. The recorder can be switched off entirely without affecting anything else.
- **Recordings stay on your PC.** Clips are written to your own `Videos\SiegeIQ` folder. Nothing is
  uploaded unless you press Send on a specific clip, or deliberately choose automatic sending in
  settings. The default is to send nothing.
- **Upload-only device token.** If it ever leaked, it could upload replays and nothing else.
  Unlink it anytime from your SiegeIQ profile.
- **Local and visible.** Config and a plain-text log live in `%APPDATA%\SiegeIQSync\` — open
  **View log** from the tray icon any time you want to see exactly what it's done. Pause or quit
  anytime from the same tray menu.
- **Almost dependency-free.** The upload/watch logic is standard-library-only Go. Outside the
  standard library: [`getlantern/systray`](https://github.com/getlantern/systray) (MIT) draws the
  tray icon and its menu; [`golang.org/x/sys`](https://pkg.go.dev/golang.org/x/sys) (the Go team's
  official low-level Windows package) backs the "run at startup" registry entry and the registry
  reads in the security check below; and [`StackExchange/wmi`](https://github.com/StackExchange/wmi)
  (plus its own dependency, `go-ole`) backs the two WMI reads in that same security check. None of
  these have any network access of their own.
- **Reports a system security posture, on request or once at launch.** A read-only check of Secure
  Boot, TPM 2.0, VBS, HVCI, and Windows build number — all values a signed-in user can already read
  without an admin prompt. It never blocks or gates the watch loop above; a failed read is logged as
  `"unknown"`, never guessed. Run it any time with `SiegeIQSync.exe -security-check`.

This repository is public so that anyone can verify those claims by reading the code. That is the
point of open-sourcing it.

## The recorder

A rolling buffer, running only while Siege is open, so the clip you wanted already exists by the
time you decide you wanted it.

- **Keeps the last 45 minutes** by default and throws the rest away. A ranked match with overtime
  fits comfortably. The length and the disk budget are both yours to set.
- **One click saves** the last 30 seconds, 1, 2, 5 or 10 minutes, or the whole match. It is cut from
  footage that already exists, not from the moment you pressed the button.
- **Cuts on real round boundaries.** Siege writes a replay file the instant each round ends, so the
  app can cut on true round endings without parsing anything at all. Round *starts* are currently
  estimated rather than measured, and every clip says so on its face rather than pretending
  otherwise.
- **Records game sound** through Windows loopback, so no extra input device is needed.

Capture uses FFmpeg, through either DXGI Desktop Duplication or GDI window capture. Which one is in
use is chosen automatically, shown in the app, and re-chosen on its own if the one in use stops
producing frames. **Exclusive fullscreen cannot be captured by either method**; the app detects that
case, says so plainly, and asks you to set Siege to Borderless. It does not pretend to be recording.

## Installing

Grab the installer from **[siegeiq.gg/sync](https://siegeiq.gg/sync)** and run it. It:

- installs **just for you** — no administrator prompt,
- lets you **choose the install folder** (or accept the default),
- can **start Sync automatically when you sign in** (a checkbox you can change later), and
- adds a normal **Uninstall** entry to Add/Remove Programs.

Prefer no installer? The raw `SiegeIQSync.exe` is published alongside it — it's fully portable, just
double-click it. Builds are **not code-signed yet**, so Windows may show a blue "Windows protected
your PC" screen the first time you run one: click **More info**, then **Run anyway**. Every release
publishes a `SHA256SUMS.txt` you can check your download against.

## How it works

1. On first run it finds your `MatchReplay` folder and shows you a 6-character pairing code.
2. You enter that code on your SiegeIQ profile (**Profile → SiegeIQ Sync → Link device**) to link it.
3. About a minute after a match folder settles, Sync uploads that match's `.rec` files through the
   exact same pipeline as a manual Verified Stats upload — same XP, same bonuses.

The tray icon's tooltip and menu always show current status (watching / uploading / paused), and
you can pause, resume, launch-at-startup, check for updates, view the log, or quit from there — no
terminal window involved.

## Staying up to date

Sync checks [siegeiq.gg](https://siegeiq.gg) for new versions on launch, and there's a **Check for
updates** item in the tray menu. When a newer build exists it offers to update: it downloads the new
version, verifies its SHA-256, swaps it in, and restarts itself — your link and settings are kept.
You can always decline and keep the current version.

## Build from source

You need [Go](https://go.dev/dl/) (any recent version) installed.

```
git clone https://github.com/ZeroHeroDinero/siegeiq-sync.git
cd siegeiq-sync
go mod tidy
go build -ldflags="-s -w -H=windowsgui" -o SiegeIQSync.exe
```

`-H=windowsgui` is what keeps the console window from popping up — SiegeIQ Sync is meant to be a
tray app, not a terminal script.

On Windows you can also just double-click **`build.bat`**, which runs the same steps (embedding the
branded `.exe` icon via [go-winres](https://github.com/tc-hib/go-winres)) and, if
[Inno Setup 6](https://jrsoftware.org/isdl.php) is installed, also builds the installer into
`installer\dist\SiegeIQSync-Setup.exe`.

### Source layout

| File | What's in it |
|------|--------------|
| `main.go` | tray icon, menu, and the watch loop |
| `replay.go` | finding `MatchReplay`, pairing, uploading (standard-library only) |
| `dialog.go` | the branded native TaskDialog popups (no GUI toolkit) |
| `startup.go` | the "launch when Windows starts" registry toggle |
| `update.go` | the built-in auto-updater |
| `security.go` | the read-only system security posture check |
| `config.go` | config/state files, paths, logging, constants |
| `recorder.go` | the recorder's own loop: when to capture, when to stop, when to cut |
| `capture_ffmpeg.go` | building the FFmpeg command line, and probing what this PC can actually do |
| `capture_watchdog.go` | noticing that capture has stopped producing anything, and recovering |
| `clipcompress.go` | making a smaller copy for upload, without touching your own file |
| `clipsend.go` | sending in the background, so the window never freezes |
| `clipupload.go` | the three-step upload: ask, send straight to storage, confirm |
| `ringbuf.go` | the rolling buffer and its disk budget |
| `rounds.go` | working out when each round happened from replay file timestamps |
| `keeprules.go` | deciding what survives out of the buffer |
| `ui_html.go` | the app window's page, as one self-contained string |
| `ui_api.go` | everything the window can ask for and everything it can do |
| `installer/` | the Inno Setup script and branded wizard images |

## Official releases

Official builds are published at **[siegeiq.gg/sync](https://siegeiq.gg/sync)** and as
[GitHub Releases](https://github.com/ZeroHeroDinero/siegeiq-sync/releases), each with a published
`SHA256SUMS.txt` you can check your download against. Code signing is not in place yet, so verifying
that checksum is currently the way to confirm a download is genuine.

## Configuration

If your replay folder isn't found automatically, set `replay_dir` in
`%APPDATA%\SiegeIQSync\config.json`.

## Reporting a problem

Open an issue here, or reach us through [siegeiq.gg](https://siegeiq.gg). For a security concern,
please open a **private** GitHub security advisory instead of a public issue — see
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 SiegeIQ
