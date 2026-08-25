; SiegeIQ Sync - Inno Setup script
; -----------------------------------------------------------------------------
; Builds SiegeIQSync-Setup.exe: a per-user installer that needs NO admin rights.
;
;   * Installs to the user's own Programs folder by default, and lets the user
;     pick a different folder on the "Select Destination Location" page.
;   * Optional shortcuts (Start Menu always; Desktop if ticked).
;   * Optional "start when I sign in to Windows" - writes the SAME registry
;     value the app itself manages (HKCU ...\Run "SiegeIQ Sync"), so the app's
;     tray toggle and this installer stay in sync.
;   * A real uninstaller (Add/Remove Programs) that also clears the Run entry.
;
; Compile with Inno Setup 6:  iscc siegeiq-sync.iss
; The compiled installer lands in installer\dist\SiegeIQSync-Setup.exe
; -----------------------------------------------------------------------------

#define MyAppName "SiegeIQ Sync"
#define MyAppVersion "1.6.3"
#define MyAppPublisher "SiegeIQ"
#define MyAppURL "https://siegeiq.gg"
#define MyAppExeName "SiegeIQSync.exe"

[Setup]
; A fresh, stable GUID identifies this app for upgrades and uninstall.
AppId={{3E820BD5-32A5-40E1-8C77-D7D2C8F18C22}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/sync
AppUpdatesURL={#MyAppURL}/sync
VersionInfoVersion={#MyAppVersion}
VersionInfoCompany={#MyAppPublisher}
VersionInfoProductName={#MyAppName}
VersionInfoDescription={#MyAppName} Setup

; --- Per-user install: no UAC / admin prompt, ever. ---
PrivilegesRequired=lowest
DefaultDirName={autopf}\SiegeIQ Sync
DisableProgramGroupPage=yes
AllowNoIcons=yes

; --- Branding ---
WizardStyle=modern
SetupIconFile=..\siegeiq_icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
UninstallDisplayName={#MyAppName}
WizardImageFile=wizard-large-100.bmp,wizard-large-150.bmp,wizard-large-200.bmp
WizardSmallImageFile=wizard-small-100.bmp,wizard-small-150.bmp,wizard-small-200.bmp
LicenseFile=..\LICENSE

; --- Output ---
OutputDir=dist
OutputBaseFilename=SiegeIQSync-Setup
Compression=lzma2/max
SolidCompression=yes
MinVersion=10.0

; If Sync is already running (e.g. an in-place reinstall), offer to close it
; cleanly rather than failing to overwrite the exe, then relaunch it after.
CloseApplications=yes
RestartApplications=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "startup"; Description: "Start {#MyAppName} automatically when I sign in to Windows"; GroupDescription: "Startup:"
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "..\SiegeIQSync.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion

; --- Recorder: the capture engine ------------------------------------------
; ffmpeg.exe is what the screen recorder uses to grab and encode frames. It is
; run as a SEPARATE PROCESS, never linked into SiegeIQSync.exe, which is what
; keeps the licensing to an attribution notice rather than a source obligation.
;
; ================================================================================
; THE BUG THIS REPLACES. Fixed 2026-08-13, after a user could not record at all.
; ================================================================================
; These two lines used to read  Source: "..\ffmpeg\ffmpeg.exe".
;
; Inno resolves a relative Source against the folder ISCC was LAUNCHED from, not
; the folder holding this .iss. Both build.bat and RELEASE_SYNC.bat launch it from
; siegeiq-sync\, so "..\ffmpeg\" meant CURRENT\ffmpeg\ - which holds the
; downloaded zip and its extracted folder, and no loose ffmpeg.exe. The real file
; has always been one level down in siegeiq-sync\ffmpeg\.
;
; skipifsourcedoesntexist then swallowed the miss in silence. EVERY release ever
; built shipped with no recorder, the installer compiled clean, the deploy
; reported success, and the only symptom was a player being told there was no
; capture engine with no way to fix it.
;
; SourcePath is an ISPP built-in: the directory containing THIS script, resolved
; at compile time. It cannot drift with the working directory.
; TAKE THE FOLDER AS AN ABSOLUTE PATH FROM THE CALLER. Second attempt, 2026-08-13.
;
; The first fix used SourcePath and still produced a 30.7 MB installer, meaning the file
; was still not being picked up. Rather than guess at how Inno resolves ".." a third time,
; both build scripts now compute the folder with %CD% and hand it over as /DFFmpegDir.
; A shell-expanded absolute path has no resolution rules left to get wrong.
;
; The SourcePath form is kept only as a fallback for someone compiling the .iss by hand.
#ifndef FFmpegDir
  #define FFmpegDir SourcePath + "..\ffmpeg"
#endif
#define FFmpegExe FFmpegDir + "\ffmpeg.exe"
#define FFmpegLic FFmpegDir + "\FFMPEG-LICENSE.txt"
#pragma message "RECORDER: looking for " + FFmpegExe

; And the miss is no longer silent. A sync-only build is still a legitimate thing
; to produce - pass /DNoRecorder to say so ON PURPOSE. Without it, a missing
; ffmpeg.exe stops the compile instead of quietly shipping a crippled installer.
#ifndef NoRecorder
  #if !FileExists(FFmpegExe)
    #error Recorder missing. ffmpeg.exe was not found next to this installer script. Read siegeiq-sync\ffmpeg\README-PUT-FFMPEG-HERE.txt, or pass /DNoRecorder to build a sync-only installer on purpose.
  #endif
  #pragma message "RECORDER: bundling " + FFmpegExe
#else
  #pragma message "RECORDER: /DNoRecorder given - building a sync-only installer"
#endif
Source: "{#FFmpegExe}"; DestDir: "{app}\ffmpeg"; Flags: ignoreversion skipifsourcedoesntexist
Source: "{#FFmpegLic}"; DestDir: "{app}\ffmpeg"; Flags: ignoreversion skipifsourcedoesntexist

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Comment: "Watch for new Siege matches and upload them to SiegeIQ"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Registry]
; Run-at-startup: identical value name + quoted path the app writes itself, so
; the tray "Launch at startup" toggle and this installer never disagree.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; \
    ValueName: "SiegeIQ Sync"; ValueData: """{app}\{#MyAppExeName}"""; \
    Flags: uninsdeletevalue; Tasks: startup

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "Launch {#MyAppName} now"; \
    Flags: nowait postinstall skipifsilent

[Code]
{ Belt-and-braces: remove the startup Run entry on uninstall even if the app
  (not the installer) is what created it, so nothing is left behind. }
procedure CurUninstallStepChanged(CurStep: TUninstallStep);
begin
  if CurStep = usUninstall then
    RegDeleteValue(HKEY_CURRENT_USER,
      'Software\Microsoft\Windows\CurrentVersion\Run', 'SiegeIQ Sync');
end;
