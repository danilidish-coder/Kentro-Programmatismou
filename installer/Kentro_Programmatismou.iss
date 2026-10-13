; Κέντρο Προγραμματισμού — per-user Windows installer.
; Stable AppId is CRITICAL: new setups upgrade this installation in place.
; Application projects and portable development runtimes are stored separately
; under %LOCALAPPDATA%\KentroOfflineStudioV2 and MUST NEVER be removed.
#define AppName "Κέντρο Προγραμματισμού"
#define AppVersion "2.0.1"
#define ExeName "Kentro_Programmatismou.exe"
#define AppGuid "{{4EA6FCDB-0FBA-46E0-9F95-E691788537A3}"

[Setup]
AppId={#AppGuid}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=Κέντρο Προγραμματισμού
DefaultDirName={localappdata}\Programs\Kentro_Programmatismou
DefaultGroupName={#AppName}
UninstallDisplayName={#AppName}
UninstallDisplayIcon={app}\{#ExeName}
OutputDir=..\installer-output
OutputBaseFilename=Kentro_Programmatismou_Setup_v2.0.1
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
DisableProgramGroupPage=yes
DisableDirPage=yes
UsePreviousAppDir=yes
WizardStyle=modern
Compression=none
SolidCompression=no
CloseApplications=yes
RestartApplications=no
SetupLogging=yes
VersionInfoVersion=2.0.1.0
VersionInfoDescription=Κέντρο Προγραμματισμού - Εγκατάσταση
VersionInfoProductName=Κέντρο Προγραμματισμού

[Files]
Source: "..\publish\Kentro_Programmatismou.exe"; DestDir: "{app}"; DestName: "{#ExeName}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#ExeName}"; WorkingDir: "{app}"
Name: "{userdesktop}\{#AppName}"; Filename: "{app}\{#ExeName}"; WorkingDir: "{app}"

[Run]
Filename: "{app}\{#ExeName}"; Description: "Άνοιγμα Κέντρου Προγραμματισμού"; Flags: nowait postinstall skipifsilent
