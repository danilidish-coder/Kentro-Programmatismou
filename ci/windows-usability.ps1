$ErrorActionPreference = 'Stop'
$exe = Join-Path $PSScriptRoot '..\build\Kentro_Programmatismou.exe'
$log = Join-Path $PSScriptRoot '..\build\windows-usability.log'
function Trace($s) {
    Add-Content -LiteralPath $log -Value $s -Encoding UTF8
    Write-Host $s
}
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class UsabilityWin32 {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern IntPtr GetDlgItem(IntPtr parent, int id);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT rect);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
    [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint msg, IntPtr w, IntPtr l);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
}
'@
function Get-ControlRect([IntPtr]$main, [int]$id) {
    $h = [UsabilityWin32]::GetDlgItem($main, $id)
    if ($h -eq [IntPtr]::Zero) { throw "Missing expected GUI control $id" }
    $r = New-Object 'UsabilityWin32+RECT'
    if (-not [UsabilityWin32]::GetWindowRect($h, [ref]$r)) { throw "Cannot locate control $id" }
    return $r
}
function Confirm-Message($title) {
    $dialog = [IntPtr]::Zero
    for ($i=0; $i -lt 50; $i++) {
        $dialog = [UsabilityWin32]::FindWindow('#32770', $title)
        if ($dialog -ne [IntPtr]::Zero) { break }
        Start-Sleep -Milliseconds 200
    }
    if ($dialog -eq [IntPtr]::Zero) { throw "Confirmation dialog did not appear: $title" }
    # Simulate the same Yes action as a human clicking the confirmation.
    if (-not [UsabilityWin32]::PostMessage($dialog, 0x0111, [IntPtr]6, [IntPtr]::Zero)) {
        throw "Could not confirm: $title"
    }
}
Trace 'Windows usability / project archive test: start'
$root = Join-Path $env:LOCALAPPDATA 'KentroProgrammatismou_Clean'
$id = 'face1234face1234face1234'
$projectRoot = Join-Path $root "projects\$id"
$removedRoot = Join-Path $root 'removed'
New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'source'),(Join-Path $projectRoot 'runs\previous'),(Join-Path $projectRoot 'stable\v1') | Out-Null
$project = @{
    id=$id; name='CI Safe Archive Fixture'; version='1.0'; language='python'; source_hash='fixture'
    imported_at='2026-10-12T09:00:00Z'
} | ConvertTo-Json -Compress
[System.IO.File]::WriteAllText((Join-Path $projectRoot 'project.json'),$project)
[System.IO.File]::WriteAllText((Join-Path $projectRoot 'source\main.py'),'print("preserved")')
[System.IO.File]::WriteAllText((Join-Path $projectRoot 'runs\previous\results.json'),'history preserved')
[System.IO.File]::WriteAllText((Join-Path $projectRoot 'stable\v1\approved.txt'),'stable preserved')
$p = Start-Process -FilePath $exe -PassThru
try {
    $hwnd = [IntPtr]::Zero
    for ($i=0;$i -lt 50;$i++) {
        Start-Sleep -Milliseconds 200
        $p.Refresh()
        if ($p.HasExited) { throw "GUI exited: $($p.ExitCode)" }
        if ($p.MainWindowHandle -ne [IntPtr]::Zero) { $hwnd=$p.MainWindowHandle; break }
    }
    if ($hwnd -eq [IntPtr]::Zero) { throw 'Desktop session failed to expose main window' }
    Trace 'PASS: native Windows GUI loaded fixture'
    $list = Get-ControlRect $hwnd 90
    $remove = Get-ControlRect $hwnd 113
    if ($list.Bottom -gt $remove.Top) { throw 'FAIL: project list overlaps Remove button' }
    Trace 'PASS: project list and Remove button do not overlap'
    # Expand tool panel to inspect the previously broken condensed result layout.
    if (-not [UsabilityWin32]::PostMessage($hwnd,0x0111,[IntPtr]112,[IntPtr]::Zero)) { throw 'Expand control failed' }
    Start-Sleep -Milliseconds 450
    $restore = [UsabilityWin32]::GetDlgItem($hwnd,114)
    if ($restore -eq [IntPtr]::Zero -or -not [UsabilityWin32]::IsWindowVisible($restore)) {throw 'Restore button not visible in advanced tools'}
    $output = Get-ControlRect $hwnd 91
    $advancedButton = Get-ControlRect $hwnd 114
    $frame = New-Object 'UsabilityWin32+RECT'
    [void][UsabilityWin32]::GetWindowRect($hwnd,[ref]$frame)
    if ($output.Top -lt $advancedButton.Bottom + 5) { throw 'FAIL: results overlap advanced buttons' }
    if ($output.Bottom -gt $frame.Bottom - 30) { throw 'FAIL: results overlap footer/window edge' }
    Trace 'PASS: expanded controls and results have separated rectangles'
    if (-not [UsabilityWin32]::PostMessage($hwnd,0x0111,[IntPtr]113,[IntPtr]::Zero)) {throw 'Remove button not accepted'}
    Confirm-Message 'Επιβεβαίωση αφαίρεσης'
    Start-Sleep -Milliseconds 450
    if (Test-Path -LiteralPath $projectRoot) { throw 'FAIL: removed project still active' }
    $archives = @(Get-ChildItem -Path $removedRoot -Directory | Where-Object {$_.Name.EndsWith("-$id")})
    if ($archives.Count -ne 1) { throw "FAIL: expected one archived project, found $($archives.Count)" }
    $stored = $archives[0].FullName
    foreach($entry in @('source\main.py','runs\previous\results.json','stable\v1\approved.txt')) {
        if (-not (Test-Path (Join-Path $stored $entry))) { throw "FAIL: lost archived data $entry" }
    }
    Trace 'PASS: GUI removal safely archives all project history'
    if (-not [UsabilityWin32]::PostMessage($hwnd,0x0111,[IntPtr]114,[IntPtr]::Zero)) {throw 'Restore button not accepted'}
    Confirm-Message 'Επαναφορά έργου'
    Start-Sleep -Milliseconds 450
    foreach($entry in @('source\main.py','runs\previous\results.json','stable\v1\approved.txt')) {
        if (-not (Test-Path (Join-Path $projectRoot $entry))) { throw "FAIL: restoration lost $entry" }
    }
    Trace 'PASS: GUI restore returns project and history'
}
finally {
    if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue }
    # Fixture belongs to ephemeral CI only, never touch unrelated projects.
    if (Test-Path -LiteralPath $projectRoot) { Remove-Item -LiteralPath $projectRoot -Recurse -Force }
    if (Test-Path -LiteralPath $removedRoot) {
        Get-ChildItem -LiteralPath $removedRoot -Directory | Where-Object {$_.Name.EndsWith("-$id")} | Remove-Item -Recurse -Force
    }
}
Trace 'PASS: Windows usability and reversible removal end-to-end'
