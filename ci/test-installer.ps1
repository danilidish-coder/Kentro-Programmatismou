# Run on a fresh Windows GitHub runner AFTER the regular self-test.
# Tests actual Inno Setup install, relaunch, upgrade (repeat setup), and uninstall.
# Never delete %LOCALAPPDATA%\KentroOfflineStudioV2 user project data.
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$installer = (Get-ChildItem installer-output -Filter 'Kentro_Programmatismou_Setup_v*.exe' -File | Select-Object -First 1).FullName
if (-not $installer) { throw 'Installer output missing' }
$source = (Resolve-Path 'publish/Kentro_Programmatismou.exe').Path
$root = Join-Path $env:LOCALAPPDATA 'KentroOfflineStudioV2'
$projectsDir = Join-Path $root 'projects'
$projectFile = Get-ChildItem -LiteralPath $projectsDir -File -Filter 'project.json' -Recurse | Select-Object -First 1
if (-not $projectFile) { throw 'QA did not create a genuine persisted project for upgrade verification' }
$preDataHash = (Get-FileHash -LiteralPath $projectFile.FullName -Algorithm SHA256).Hash
$preDataFolders = @(Get-ChildItem -LiteralPath $projectsDir -Directory).Count
$installedDir = Join-Path $env:LOCALAPPDATA 'Programs\Kentro_Programmatismou'
$installed = Join-Path $installedDir 'Kentro_Programmatismou.exe'
$desktop = [Environment]::GetFolderPath('DesktopDirectory')
$shortcut = Join-Path $desktop 'Κέντρο Προγραμματισμού.lnk'
$start = Join-Path ([Environment]::GetFolderPath('Programs')) 'Κέντρο Προγραμματισμού.lnk'
$srcHash = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
$log = [System.Collections.Generic.List[string]]::new()
function Record([string] $note) {
  $log.Add($note)
  Write-Host $note
}
function Require([bool] $condition, [string] $description) {
  if (-not $condition) { throw "INSTALLER FAIL: $description" }
  Record "PASS: $description"
}
function CheckProjects([string] $phase) {
  Require (Test-Path -LiteralPath $projectFile.FullName) "$phase - project remains present"
  $hash = (Get-FileHash -LiteralPath $projectFile.FullName -Algorithm SHA256).Hash
  Require ($hash -eq $preDataHash) "$phase - exact SHA256 project data preserved"
  Require (@(Get-ChildItem -LiteralPath $projectsDir -Directory).Count -ge $preDataFolders) "$phase - existing project directories preserved"
}
function LaunchInstaller([string] $phase) {
  $args = @('/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART','/CLOSEAPPLICATIONS',"/LOG=$env:TEMP\kentro-installer-$phase.log")
  $p = Start-Process -FilePath $installer -ArgumentList $args -PassThru -Wait
  Require ($p.ExitCode -eq 0) "$phase installer exited successfully"
  Require (Test-Path -LiteralPath $installed) "$phase EXE installed to local per-user directory"
  Require ((Get-FileHash -LiteralPath $installed -Algorithm SHA256).Hash -eq $srcHash) "$phase installed EXE is byte-identical to CI verified executable"
  Require (Test-Path -LiteralPath $shortcut) "$phase Greek desktop shortcut exists"
  Require (Test-Path -LiteralPath $start) "$phase Greek Start Menu shortcut exists"
  # Shortcut target metadata may be blank for Inno-generated links on
  # some CI runners. Verify by a real shell launch below instead of trusting
  # WScript.Shell metadata to reject a working shortcut.
  CheckProjects $phase
}
try {
  Require (Test-Path -LiteralPath $installer) 'Installer setup EXE exists'
  Require ((Get-Item -LiteralPath $installer).Length -gt 150000000) 'Installer contains complete portable application payload'
  LaunchInstaller 'first-install'
  # Open the installed application through the REAL desktop shortcut,
  # exactly as the end user will. Only that proves the shortcut is functional.
  $beforePids = @(Get-Process -Name Kentro_Programmatismou -ErrorAction SilentlyContinue | ForEach-Object {$_.Id})
  Start-Process -FilePath $shortcut
  $p = $null
  for($i=0; $i -lt 45; $i++) {
    Start-Sleep -Seconds 1
    $candidates = @(Get-Process -Name Kentro_Programmatismou -ErrorAction SilentlyContinue |
      Where-Object {$beforePids -notcontains $_.Id})
    foreach($candidate in $candidates) {
      if($candidate.MainWindowHandle -ne 0) {$p=$candidate;break}
    }
    if($null -ne $p) {break}
  }
  Require ($null -ne $p) 'Installed GUI starts from Greek desktop shortcut with a real Windows main window'
  $p.Refresh()
  $runningPath = (Get-CimInstance Win32_Process -Filter "ProcessId=$($p.Id)").ExecutablePath
  Require (-not [string]::IsNullOrWhiteSpace($runningPath)) 'Shortcut resolves to a real executable path'
  Require ([IO.Path]::GetFullPath($runningPath) -eq [IO.Path]::GetFullPath($installed)) 'Shortcut launches exact installed EXE, not old portable copy'
  try {$p.CloseMainWindow() | Out-Null} catch {}
  Start-Sleep -Seconds 2
  $p.Refresh()
  if(-not $p.HasExited) {Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue}
  CheckProjects 'after-gui-launch'

  LaunchInstaller 'in-place-upgrade'
  # The second installation must not create a second Windows app entry.
  $entries = @(Get-ChildItem 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall' |
    Get-ItemProperty | Where-Object {$_.DisplayName -eq 'Κέντρο Προγραμματισμού'})
  Require ($entries.Count -eq 1) 'In-place upgrade creates ONE Windows uninstall entry'

  $uninstaller = Get-ChildItem -LiteralPath $installedDir -File -Filter 'unins*.exe' | Select-Object -First 1
  Require ($null -ne $uninstaller) 'Windows uninstaller created'
  $p = Start-Process -FilePath $uninstaller.FullName -ArgumentList @('/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART') -PassThru -Wait
  Require ($p.ExitCode -eq 0) 'Uninstall completes without administrative access'
  Require (-not (Test-Path -LiteralPath $installed)) 'Uninstall removes program executable'
  Require (-not (Test-Path -LiteralPath $shortcut)) 'Uninstall removes managed desktop shortcut'
  CheckProjects 'after-uninstall'
  Record 'ALL INSTALLER WINDOWS TESTS PASSED'
}
finally {
  $log | Set-Content -LiteralPath 'installer-windows-qa.log' -Encoding utf8
}
