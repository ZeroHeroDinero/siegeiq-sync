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
#define MyAppVersion "0.3.3"
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
