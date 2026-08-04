// SiegeIQ Sync - watches the Siege replay folder and uploads new matches.
//
// TRUST GUARANTEES (also shown in the app - never weaken these):
//   - Reads files only. Never touches the game process, memory, or network traffic.
//   - Watches exactly one directory tree: ...\My Games\Rainbow Six - Siege\<id>\MatchReplay
//   - Uploads only replay (.rec) files. Nothing else on disk.
//   - Pause anytime from the tray icon. Unlink anytime from siegeiq.gg (Profile -> SiegeIQ Sync).
//
// Sync lives in the system tray - there is no console window (unless launched
// with -security-check, see below). Status shows up in the tray tooltip/menu
// and the plain-text log (%APPDATA%\SiegeIQSync\sync.log, opened by the "View
// log" tray item). Dependencies outside the Go standard library:
// github.com/getlantern/systray (MIT) for the tray icon and menu;
// golang.org/x/sys (the Go team's official low-level Windows package) for the
// run-at-startup registry entry and the registry reads in security.go; and
// github.com/StackExchange/wmi (plus its own dependency,
// github.com/go-ole/go-ole) for the two read-only WMI queries in security.go
// (TPM and Device Guard status). Every one of those reads local OS/firmware
// configuration only - see security.go's header for the exact boundary.
// Nothing here changes the trust guarantees above.
//
// The code is split by concern for easy reading:
//
//	config.go   - config/state files, paths, logging, constants
//	replay.go   - finding MatchReplay, pairing, uploading (standard-library only)
//	dialog.go   - the branded native TaskDialog popups (no GUI toolkit)
//	startup.go  - the "launch when Windows starts" registry toggle
//	update.go   - the built-in auto-updater
//	security.go - the read-only system security posture check
//	main.go     - the tray icon, menu, and the watch loop (this file)
//
// Build: see README.md / build.bat. Build with -ldflags="-H=windowsgui" to hide
// the console window.
package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/getlantern/systray"
)

func main() {
	securityCheck := flag.Bool("security-check", false,
		"run the read-only system security posture check, print JSON, and exit")
	flag.Parse()
	if *securityCheck {
		printSecurityCheckAndExit()
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconICO)
	systray.SetTitle("")
	systray.SetTooltip("SiegeIQ Sync v" + version + " - starting up...")

	mStatus := systray.AddMenuItem("Starting up...", "Current status")
	mStatus.Disable()
	systray.AddSeparator()
	mPause := systray.AddMenuItem("Pause syncing", "Stop uploading new matches until resumed")
	mStartup := systray.AddMenuItemCheckbox("Launch at startup", "Start SiegeIQ Sync automatically when Windows starts", startupEnabled())
	prefSound, prefToast := notifyPrefs()
	setNotifySound(prefSound)
	setNotifyToast(prefToast)
	mSound := systray.AddMenuItemCheckbox("Play a sound on upload",
		"A short chime when a match uploads, a sharper one if it fails", prefSound)
	mToast := systray.AddMenuItemCheckbox("Show a notification on upload",
		"A tray balloon when a match uploads. Never steals focus from the game", prefToast)
	mUpdate := systray.AddMenuItem("Check for updates", "Check siegeiq.gg for a newer version")
	mOpenSite := systray.AddMenuItem("Open siegeiq.gg", "Open your SiegeIQ profile")
	mViewLog := systray.AddMenuItem("View log", "Open the plain-text sync log")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit SiegeIQ Sync", "Stop syncing and exit")

	go runSync(mStatus, mStartup)

	go func() {
		for {
			select {
			case <-mPause.ClickedCh:
				if atomic.LoadInt32(&paused) == 0 {
					atomic.StoreInt32(&paused, 1)
					mPause.SetTitle("Resume syncing")
					mStatus.SetTitle("Paused - not watching for matches")
					systray.SetTooltip("SiegeIQ Sync - paused")
					logf("paused from the tray menu")
				} else {
					atomic.StoreInt32(&paused, 0)
					mPause.SetTitle("Pause syncing")
					mStatus.SetTitle("Watching for matches...")
					systray.SetTooltip("SiegeIQ Sync - watching for matches")
					logf("resumed from the tray menu")
				}

			case <-mStartup.ClickedCh:
				if mStartup.Checked() {
					if err := setStartup(false); err != nil {
						logf("could not turn off run-at-startup: %v", err)
					} else {
						mStartup.Uncheck()
						logf("run-at-startup turned off from the tray")
					}
				} else {
					if err := setStartup(true); err != nil {
						logf("could not turn on run-at-startup: %v", err)
					} else {
						mStartup.Check()
						logf("run-at-startup turned on from the tray")
					}
				}

			case <-mSound.ClickedCh:
				on := !mSound.Checked()
				setNotifySound(on)
				if on {
					mSound.Check()
					beep(true) // so the choice is audible the moment it is made
				} else {
					mSound.Uncheck()
				}
				saveNotifyPrefs()
				logf("upload sound turned %v from the tray", onOff(on))

			case <-mToast.ClickedCh:
				on := !mToast.Checked()
				setNotifyToast(on)
				if on {
					mToast.Check()
				} else {
					mToast.Uncheck()
				}
				saveNotifyPrefs()
				logf("upload notification turned %v from the tray", onOff(on))

			case <-mUpdate.ClickedCh:
				// Network + a dialog: run off the event loop so the menu stays responsive.
				go func() {
					if info := updateAvailable(); info != nil {
						promptAndUpdate(info)
					} else {
						showDialog(dialogSpec{
							instruction: "You're up to date",
							content:     fmt.Sprintf("SiegeIQ Sync v%s is the latest version.", version),
							buttonText:  "Close",
						})
					}
				}()

			case <-mOpenSite.ClickedCh:
				openURL("https://siegeiq.gg")

			case <-mViewLog.ClickedCh:
				openInNotepad(filepath.Join(configDir(), "sync.log"))

			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	// Nothing to clean up - config/state are saved as they change, not on exit.
}

// runSync holds the setup + watch loop. It runs in its own goroutine so the
// tray's event loop (systray.Run) stays responsive the whole time.
func runSync(mStatus, mStartup *systray.MenuItem) {
	logf("SiegeIQ Sync v%s starting", version)

	// Read-only, non-blocking - logs one JSON line to sync.log and never
	// affects the watch loop below, whatever it finds.
	go runSecurityCheckOnce()

	// Clean up after any previous self-update, then check for a new one before
	// we do anything else - if the player updates, we restart into the new build.
	cleanupOldExe()
	if info := updateAvailable(); info != nil {
		logf("update available: v%s", info.Version)
		mStatus.SetTitle("Update available - v" + info.Version)
		promptAndUpdate(info) // may install and restart, exiting this process
	}

	cfgPath := filepath.Join(configDir(), "config.json")
	stPath := filepath.Join(configDir(), "state.json")
	cfg := &config{}
	st := &state{Uploaded: map[string]string{}}
	loadJSON(cfgPath, cfg)
	loadJSON(stPath, st)
	setNotifySound(!cfg.NotifySoundOff)
	setNotifyToast(!cfg.NotifyToastOff)
	if st.Uploaded == nil {
		st.Uploaded = map[string]string{}
	}

	if cfg.ReplayDir == "" {
		cfg.ReplayDir = findReplayDir()
		if cfg.ReplayDir == "" {
			mStatus.SetTitle("Setup needed - see the popup")
			logf("could not find the MatchReplay folder - is Siege installed and has a match been played?")
			showDialog(dialogSpec{
				instruction: "Setup needed",
				content: "Couldn't find your Rainbow Six Siege replay folder.\r\n\r\n" +
					"Make sure Siege is installed and you've played at least one match, then quit and reopen " +
					"SiegeIQ Sync from the tray icon.\r\n\r\n" +
					"You can also set the folder manually in:\r\n" + cfgPath,
				footer:       "Need a hand? Visit siegeiq.gg/sync",
				openSiteURL:  "https://siegeiq.gg/sync",
				openSiteText: "Open siegeiq.gg/sync",
				buttonText:   "Close",
			})
			return
		}
		saveJSON(cfgPath, cfg)
	}
	logf("watching: %s", cfg.ReplayDir)

	if cfg.DeviceToken == "" {
		mStatus.SetTitle("Waiting to be linked - see the popup")
		if err := pair(cfg, cfgPath, mStatus); err != nil {
			mStatus.SetTitle("Pairing failed - see the log")
			logf("pairing failed: %v", err)
			return
		}
		// Reflect any run-at-startup choice made in the pairing popup, and
		// celebrate a successful first link.
		if startupEnabled() {
			mStartup.Check()
		} else {
			mStartup.Uncheck()
		}
		notifyPaired()
		showDialog(dialogSpec{
			instruction: "You're linked!",
			content: "SiegeIQ Sync is now watching for new matches. After each one, it uploads the replay on " +
				"its own - your Verified Stats update automatically.\r\n\r\n" +
				"You can close this window; Sync keeps running in the tray (the SiegeIQ icon near the clock).",
			footer:       "Pause, view the log, or quit any time from the tray icon.",
			openSiteURL:  "https://siegeiq.gg",
			openSiteText: "Open siegeiq.gg",
			buttonText:   "Great",
		})
	}

	mStatus.SetTitle("Watching for matches...")
	systray.SetTooltip("SiegeIQ Sync - watching for matches")

	for {
		if atomic.LoadInt32(&paused) == 1 {
			time.Sleep(scanEvery)
			continue
		}
		names, err := readReplayEntries(cfg.ReplayDir)
		if err != nil {
			logf("cannot read replay folder: %v", err)
			time.Sleep(scanEvery)
			continue
		}
		for _, name := range names {
			if st.Uploaded[name] != "" {
				continue
			}
			files, ready := matchReady(filepath.Join(cfg.ReplayDir, name))
			if !ready {
				continue
			}
			logf("new match: %s (%d round files) - uploading...", name, len(files))
			mStatus.SetTitle("Uploading " + name + "...")
			systray.SetTooltip("SiegeIQ Sync - uploading a match")
			done, err := upload(cfg, files)
			if err != nil {
				logf("  %v", err)
				if isRepairNeeded(err) {
					cfg.DeviceToken = ""
					saveJSON(cfgPath, cfg)
					mStatus.SetTitle("Unlinked - re-pairing...")
					if perr := pair(cfg, cfgPath, mStatus); perr != nil {
						logf("re-pairing failed: %v", perr)
						time.Sleep(scanEvery)
					}
					continue
				}
			}
			if done {
				if err == nil {
					st.Uploaded[name] = "ok"
					logf("  synced OK")
					notifyUploadOK(name)
				} else {
					st.Uploaded[name] = "failed"
					notifyUploadFailed(name, shortReason(err))
				}
				saveJSON(stPath, st)
			}
			if atomic.LoadInt32(&paused) == 0 {
				mStatus.SetTitle("Watching for matches...")
				systray.SetTooltip("SiegeIQ Sync - watching for matches")
			}
		}
		time.Sleep(scanEvery)
	}
}
