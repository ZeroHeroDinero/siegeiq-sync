# SiegeIQ Sync

The official desktop companion for [SiegeIQ](https://siegeiq.gg). A tiny Windows app that watches
your Rainbow Six Siege replay folder and automatically uploads each new match to your SiegeIQ
account — so your Verified Stats update on their own. Play, tab out, done.

Dependency-free Go, standard library only. Windows 10 / 11.

## What it does — and what it never does

**It reads files only.** SiegeIQ Sync never touches the game process, its memory, or its network
traffic. It is a file reader, nothing more — which is exactly why it is irrelevant to BattlEye.

- **Watches exactly one folder:** `Documents\My Games\Rainbow Six - Siege\<id>\MatchReplay`
- **Uploads only `.rec` files.** Nothing else on your disk is read or sent.
- **Upload-only device token.** If it ever leaked, it could upload replays and nothing else.
  Unlink it anytime from your SiegeIQ profile.
- **Local and visible.** Config and a plain-text log live in `%APPDATA%\SiegeIQSync\`. Pause
  anytime with Ctrl+C.

This repository is public so that anyone can verify those claims by reading the code. That is the
point of open-sourcing it.

## How it works

1. On first run it finds your `MatchReplay` folder and prints a 6-character pairing code.
2. You enter that code on your SiegeIQ profile (**Profile → SiegeIQ Sync → Link device**) to link it.
3. About a minute after a match folder settles, Sync uploads that match's `.rec` files through the
   exact same pipeline as a manual Verified Stats upload — same XP, same bonuses.

## Build from source

You need [Go](https://go.dev/dl/) (any recent version) installed.

```
git clone https://github.com/YOUR-USERNAME/siegeiq-sync.git
cd siegeiq-sync
go build -ldflags="-s -w" -o SiegeIQSync.exe
```

On Windows you can also just double-click `build.bat`.

## Official releases

Official builds are published at **[siegeiq.gg/sync](https://siegeiq.gg/sync)**, code-signed through
the [SignPath Foundation](https://signpath.org) free signing program for open-source projects, with
a published SHA-256 you can check against your download.

## Configuration

If your replay folder isn't found automatically, set `replay_dir` in
`%APPDATA%\SiegeIQSync\config.json`.

## Reporting a problem

Open an issue here, or reach us through [siegeiq.gg](https://siegeiq.gg). For a security concern,
please open a **private** GitHub security advisory instead of a public issue — see
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 SiegeIQ
