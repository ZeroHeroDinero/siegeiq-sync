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
	"strings"
	"sync/atomic"
	"time"

	"github.com/getlantern/systray"
)

func main() {
	securityCheck := flag.Bool("security-check", false,
		"run the read-only system security posture check, print JSON, and exit")
	startupLaunch := flag.Bool("startup", false,
		"set by the run-at-startup registry entry; starts quietly in the tray without opening the window")
	flag.Parse()
	launchedByWindows = *startupLaunch
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
	// First real item in the menu: the app window is now the main way to use
	// Sync. The tray stays as the quick controls and the always-there presence.
	mOpenApp := systray.AddMenuItem("Open SiegeIQ Sync", "Settings, your clips, and what the recorder is doing")
	systray.AddSeparator()
	mPause := systray.AddMenuItem("Pause syncing", "Stop uploading new matches until resumed")
	registerSyncPauseItem(mPause)
	mStartup := systray.AddMenuItemCheckbox("Launch at startup", "Start SiegeIQ Sync automatically when Windows starts", startupEnabled())
	prefSound, prefToast := notifyPrefs()
	setNotifySound(prefSound)
	setNotifyToast(prefToast)
	mSound := systray.AddMenuItemCheckbox("Play a sound when a clip is saved",
		"One short chime, and only when footage was actually saved. Nothing else makes a sound", prefSound)
	mToast := systray.AddMenuItemCheckbox("Show a notification on upload",
		"A tray balloon when a match uploads. Never steals focus from the game", prefToast)
	// Settings sharing. Two switches, both off until asked, plus a way to see
	// the actual file rather than take our word for what is in it.
	prefAim, prefPerf, _ := settingsSharePrefs()
	setSettingsAimOn(prefAim)
	setSettingsPerfOn(prefPerf)
	mShare := systray.AddMenuItem("Share my Siege settings",
		"Let coaching read your in-game settings. Both off unless you switch them on")
	mShareAim := mShare.AddSubMenuItemCheckbox("Aim settings",
		"Sensitivity, ADS, FOV and raw input, so coaching can tell you when a sens change hurt your aim", prefAim)
	mSharePerf := mShare.AddSubMenuItemCheckbox("Performance settings",
		"Graphics and display quality only. Never your hardware details or your account", prefPerf)
	mShareWhat := mShare.AddSubMenuItem("Show me the file it reads",
		"Opens your GameSettings.ini so you can see exactly what is in it")
	mUpdate := systray.AddMenuItem("Check for updates", "Check siegeiq.gg for a newer version")
	mOpenSite := systray.AddMenuItem("Open siegeiq.gg", "Open your SiegeIQ profile")
	mViewLog := systray.AddMenuItem("View log", "Open the plain-text sync log")

	// The recorder owns its own menu and its own switches. Nothing in here
	// touches syncing, and nothing in the sync menu above touches the recorder.
	// All four combinations are valid: sync only, recorder only, both, neither.
	systray.AddSeparator()
	recMenu := buildRecorderMenu()

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit SiegeIQ Sync", "Stop syncing and exit")

	go recMenu.run()
	go runSync(mStatus, mStartup)
	go startSettingsWatch()

	go func() {
		for {
			select {
			case <-mPause.ClickedCh:
				// The label is set by setSyncPaused so the app window's copy of
				// this control stays in step with the menu.
				if toggleSyncPause() {
					mStatus.SetTitle("Paused - not uploading new matches")
					systray.SetTooltip("SiegeIQ Sync - uploads paused")
				} else {
					mStatus.SetTitle("Watching for matches...")
					systray.SetTooltip("SiegeIQ Sync - watching for matches")
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

			case <-mShareAim.ClickedCh:
				on := !mShareAim.Checked()
				setSettingsAimOn(on)
				if on {
					mShareAim.Check()
				} else {
					mShareAim.Uncheck()
				}
				saveSettingsSharePrefs(-1)
				logf("aim settings sharing turned %v from the tray", onOff(on))

			case <-mSharePerf.ClickedCh:
				on := !mSharePerf.Checked()
				setSettingsPerfOn(on)
				if on {
					mSharePerf.Check()
				} else {
					mSharePerf.Uncheck()
				}
				saveSettingsSharePrefs(-1)
				logf("performance settings sharing turned %v from the tray", onOff(on))

			case <-mShareWhat.ClickedCh:
				// Disk access and a possible dialog: off the event loop so the
				// menu cannot freeze while Notepad starts.
				go func() {
					if p := settingsFileForPlayer(); p != "" {
						openInNotepad(p)
					} else {
						showDialog(dialogSpec{
							instruction: "Could not find your Siege settings file",
							content:     "Sync looks for GameSettings.ini next to your replay folder. Launch Siege once and try again.",
							buttonText:  "Close",
						})
					}
				}()

			case <-mUpdate.ClickedCh:
				// Network + a dialog: run off the event loop so the menu stays responsive.
				go func() {
					if info := updateAvailable(); info != nil {
						// Same silent path as startup. Somebody who clicked "Check for
						// updates" has already said yes; asking again is a second
						// chance to accidentally decline.
						autoUpdate(info)
					} else {
						showDialog(dialogSpec{
							instruction: "You're up to date",
							content:     fmt.Sprintf("SiegeIQ Sync v%s is the latest version.", version),
							buttonText:  "Close",
						})
					}
				}()

			case <-mOpenApp.ClickedCh:
				openAppWindow()

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

// onExit runs when the tray's Quit item is used.
//
// "Nothing to clean up" is what this used to say, and it was wrong in the most
// expensive way available: ffmpeg is a CHILD PROCESS, and on Windows a child
// does not die with its parent. Quitting the app left it recording the screen
// indefinitely. Three such orphans were found still writing to disk long after
// the app that started them had been replaced.
//
// The kernel-level guarantee lives in capture_joblimit_windows.go and covers
// crashes and force-kills too. This is the polite path that runs first when
// there IS time to be polite: asking ffmpeg to finish means the last few
// seconds of footage get a proper index and stay playable, where being
// terminated would truncate them into an unplayable file.
func onExit() {
	rec.stopCapture("SiegeIQ Sync is closing")
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
	// If the previous build updated us into existence, say so once. Read BEFORE the
	// update check below so a marker can never be left behind by an immediate re-update.
	if from := consumeUpdatedMarker(); from != "" {
		logf("started after a self-update into v%s", from)
		notifyUpdated(from)
	}
	if info := updateAvailable(); info != nil {
		logf("update available: v%s", info.Version)
		mStatus.SetTitle("Updating to v" + info.Version + "...")
		autoUpdate(info) // installs and restarts, exiting this process
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
	if st.Clipped == nil {
		st.Clipped = map[string]string{}
	}

	// The recorder starts here, before pairing and before the replay folder has
	// even been located. It needs no device token to buffer footage or cut
	// clips, so somebody who has not linked their account yet still gets a
	// working recorder - and somebody whose pairing has lapsed does not lose
	// their footage on top of losing their uploads.
	rec.configure(cfg)
	logf("recorder: %s", recorderSummary(cfg.Recorder))
	go rec.run()
	// Keeps rec.tournamentActive honest so ModeTournament actually does something.
	// Cheap when idle and silent unless the answer changes.
	go watchTournament()

	// Opening the window is about INTENT, not about whether it has ever been
	// opened before. Somebody who double-clicks the app wants to see it, every
	// time. Somebody who just signed in to Windows did not ask for anything and
	// should get a tray icon and silence.
	//
	// The earlier "only ever once" rule got this wrong in both directions: it
	// ignored a deliberate launch, and it would have thrown a window at every
	// new install on sign-in.
	if !launchedByWindows {
		cfg.WindowShown = true
		saveJSON(cfgPath, cfg)
		openAppWindow()
	} else {
		logf("started by Windows at sign-in - staying in the tray")
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
		// NOTE: the scan itself is no longer skipped while syncing is paused.
		// The recorder needs to see settled matches even when nothing is being
		// uploaded, and the pause check has moved down to guard only the upload.
		names, err := readReplayEntries(cfg.ReplayDir)
		if err != nil {
			logf("cannot read replay folder: %v", err)
			time.Sleep(scanEvery)
			continue
		}
		for _, name := range names {
			dir := filepath.Join(cfg.ReplayDir, name)
			files, ready := matchReady(dir)
			if !ready {
				continue
			}

			// THE UPLOAD RUNS FIRST NOW, AND THE ORDER IS THE WHOLE POINT.
			//
			// Trimming used to happen here, before the upload, which was fine while
			// every keep rule could be answered from the .rec files' timestamps alone.
			// Three of them cannot: "only rounds I died in", "only rounds I got a kill"
			// and "only my clutch rounds" need to know what happened inside the round,
			// and the only place this app can learn that is the reply to the upload.
			// Cutting before that reply arrives means those rules can NEVER fire - they
			// degrade on every match, for ever, with no error anywhere to explain it.
			//
			// Waiting costs nothing: the buffer holds 45 minutes and an upload takes
			// seconds. Wrapped in a closure so the early-outs stay early-outs rather
			// than becoming three levels of nested if.
			ev := func() *matchEvents {
				if atomic.LoadInt32(&paused) == 1 {
					return nil
				}
				if prev := st.Uploaded[name]; prev != "" {
					// A match that was uploaded and has since GROWN means rounds were
					// sent before the match finished - which is exactly what the old
					// 45-second settle rule did on every live game. Re-sending is not
					// done automatically yet, because whether the backend treats a
					// second POST of the same match as an update or as a duplicate has
					// not been confirmed, and quietly doubling somebody's Verified
					// Stats would be a worse bug than the one being fixed. So it says
					// so, loudly, once.
					if n, ok := uploadedCount(prev); ok && len(files) > n {
						if st.Uploaded[name+"#short"] == "" {
							st.Uploaded[name+"#short"] = "logged"
							saveJSON(stPath, st)
							logf("NOTE: %s was uploaded with %d round file(s) and now has %d. "+
								"The earlier upload was incomplete - this match needs re-sending once "+
								"the backend is confirmed to replace rather than duplicate.", name, n, len(files))
						}
					}
					return nil
				}
				logf("new match: %s (%d round files) - uploading...", name, len(files))
				mStatus.SetTitle("Uploading " + name + "...")
				systray.SetTooltip("SiegeIQ Sync - uploading a match")
				done, events, err := upload(cfg, files)
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
						return nil
					}
				}
				if done {
					if err == nil {
						st.Uploaded[name] = fmt.Sprintf("ok:%d", len(files))
						logf("  synced OK (%d round files)", len(files))
						notifyUploadOK(name)
					} else {
						st.Uploaded[name] = "failed"
						// An unlinked device is not a normal upload failure and must not read
						// like one. "Sync will try again" is actively misleading here: it will
						// try forever and never succeed until the player re-pairs.
						if strings.Contains(err.Error(), "unlinked") {
							notifyUnlinked()
						} else {
							notifyUploadFailed(name, shortReason(err))
						}
					}
					saveJSON(stPath, st)
				}
				if atomic.LoadInt32(&paused) == 0 {
					mStatus.SetTitle("Watching for matches...")
					systray.SetTooltip("SiegeIQ Sync - watching for matches")
				}
				if events != nil {
					logf("  read round detail for %d round(s) - the keep rules that need it can run",
						len(events.Rounds))
				}
				return events
			}()

			// The recorder sees every settled match exactly once, whether or not
			// uploading is paused. Cutting footage on round boundaries needs only
			// these files' timestamps, which are already here on disk - nothing has
			// to be uploaded, parsed server-side, or even opened. `ev` is a bonus:
			// present, it cuts exact rounds; absent, it cuts estimated ones and says so.
			if st.Clipped[name] == "" {
				st.Clipped[name] = "seen"
				saveJSON(stPath, st)
				rec.handleMatch(dir, files, ev)
			}
		}
		time.Sleep(scanEvery)
	}
}
