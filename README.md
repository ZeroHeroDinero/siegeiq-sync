# SiegeIQ Sync

The official desktop companion for [SiegeIQ](https://siegeiq.gg). A tiny Windows app that watches
your Rainbow Six Siege replay folder and automatically uploads each new match to your SiegeIQ
account — so your Verified Stats update on their own. Play, tab out, done.

Lives quietly in your system tray. Windows 10 / 11.

## What it does — and what it never does

**It reads files only.** SiegeIQ Sync never touches the game process, its memory, or its network
traffic. It is a file reader, nothing more — which is exactly why it is irrelevant to BattlEye.

- **Watches exactly one folder:** `Documents\My Games\Rainbow Six - Siege\<id>\MatchReplay`
- **Uploads only `.rec` files.** Nothing else on your disk is read or sent.
- **Upload-only device token.** If it ever leaked, it could upload replays and nothing else.
  Unlink it anytime from your SiegeIQ profile.
- **Local and visible.** Config and a plain-text log live in `%APPDATA%\SiegeIQSync\` — open
  **View log** from the tray icon any time you want to see exactly what it's done. Pause or quit
  anytime from the same tray menu.
- **Almost dependency-free.** The upload/watch logic is standard-library-only Go. The only things
  pulled in from outside the standard library are [`getlantern/systray`](https://github.com/getlantern/systray)
  (MIT), used strictly to draw the tray icon and its menu, and its own dependency
  [`golang.org/x/sys`](https://pkg.go.dev/golang.org/x/sys) (the Go team's official low-level
  Windows package), which we also use for the one-line "run at startup" registry entry. Neither has
  any network access of its own.

This repository is public so that anyone can verify those claims by reading the code. That is the
point of open-sourcing it.

## Installing

Grab the installer from **[siegeiq.gg/sync](https://siegeiq.gg/sync)** and run it. It:

- installs **just for you** — no administrator prompt,
- lets you **choose the install folder** (or accept the default),
- can **start Sync automatically when you sign in** (a checkbox you can change later), and
- adds a normal **Uninstall** entry to Add/Remove Programs.

Prefer no installer? The raw `SiegeIQSync.exe` is published alongside it — it's fully portable, just
double-click it. Either way, downloads are code-signed with a published SHA-256 you can check.

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
| `config.go` | config/state files, paths, logging, constants |
| `installer/` | the Inno Setup script and branded wizard images |

## Official releases

Official builds are published at **[siegeiq.gg/sync](https://siegeiq.gg/sync)** and as
[GitHub Releases](https://github.com/ZeroHeroDinero/siegeiq-sync/releases), code-signed through the
[SignPath Foundation](https://signpath.org) free signing program for open-source projects, with a
published SHA-256 you can check against your download. Each push and tag is built by GitHub Actions
straight from this source, so the download matches the code you can read here.

## Configuration

If your replay folder isn't found automatically, set `replay_dir` in
`%APPDATA%\SiegeIQSync\config.json`.

## Reporting a problem

Open an issue here, or reach us through [siegeiq.gg](https://siegeiq.gg). For a security concern,
please open a **private** GitHub security advisory instead of a public issue — see
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 SiegeIQ
