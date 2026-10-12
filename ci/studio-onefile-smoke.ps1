$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$exe = Join-Path $PSScriptRoot '..\single\Kentro_Programmatismou.exe'
$log = Join-Path $PSScriptRoot '..\studio-onefile-smoke.log'
$env:KENTRO_STUDIO_DIAG_DIR = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if(-not (Test-Path $exe)) { throw 'Single EXE is missing' }
"Testing first launch and cached launch of standalone EXE." | Set-Content $log
for ($pass=1; $pass -le 2; $pass++) {
  $parent = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe) -PassThru
  $gui = $null
  try {
    for ($i=0; $i -lt 160; $i++) {
      Start-Sleep -Milliseconds 500
      $parent.Refresh()
      if ($parent.HasExited) { throw "Single EXE exited prematurely (pass=$pass, code=$($parent.ExitCode))" }
      $gui = Get-Process -Name 'Kentro_Programmatismou' -ErrorAction SilentlyContinue |
        Where-Object { $_.Id -ne $parent.Id -and $_.MainWindowHandle -ne [IntPtr]::Zero } |
        Select-Object -First 1
      if ($gui) { break }
    }
    if (-not $gui) { throw "No WinUI child window from single EXE (pass=$pass)" }
    Start-Sleep -Seconds 2
    $parent.Refresh()
    $gui.Refresh()
    if($parent.HasExited -or $gui.HasExited) { throw "WinUI child crashed (pass=$pass)" }
    $cache = Join-Path $env:LOCALAPPDATA 'KentroProgrammatismou_Clean\installed-app'
    $installed = Get-ChildItem -Path $cache -Directory -ErrorAction SilentlyContinue |
      Where-Object { Test-Path (Join-Path $_.FullName '.installed-sha256') }
    if(-not $installed) { throw 'Bundled files were not installed in local cache' }
    $hasEngine = $false
    foreach($item in $installed) {
      if(Test-Path (Join-Path $item.FullName 'Kentro_Engine.exe')) { $hasEngine = $true }
    }
    if(-not $hasEngine) { throw 'Embedded Go engine is not in extracted app' }
    "PASS: run $pass; launcher PID=$($parent.Id); GUI PID=$($gui.Id); hwnd=$($gui.MainWindowHandle)" |
      Tee-Object -FilePath $log -Append
    if($pass -eq 1) {
      $h = $gui.MainWindowHandle
      Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class OneFileBounds {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr handle, out Rect r);
  [StructLayout(LayoutKind.Sequential)] public struct Rect { public int L,T,R,B; }
}
'@
      $rect = [OneFileBounds+Rect]::new()
      if(-not [OneFileBounds]::GetWindowRect($h,[ref]$rect)) { throw 'Unable to inspect single-EXE GUI rectangle' }
      $screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
      if ($rect.L -lt $screen.Left -or $rect.T -lt $screen.Top -or $rect.R -gt $screen.Right -or $rect.B -gt $screen.Bottom) {
        throw 'Single-EXE GUI exceeds visible desktop'
      }
      "PASS: GUI within visible desktop" | Tee-Object -FilePath $log -Append
    }
    if(-not $gui.CloseMainWindow()) { throw "WinUI child could not be closed (pass=$pass)" }
    if(-not $gui.WaitForExit(20000)) { throw "WinUI child failed to exit on close (pass=$pass)" }
    if(-not $parent.WaitForExit(20000)) { throw "Launcher failed to exit after GUI close (pass=$pass)" }
    if($parent.ExitCode -ne 0) { throw "Standalone launcher returned exit code $($parent.ExitCode) (pass=$pass)" }
  }
  finally {
    try { if ($gui -and -not $gui.HasExited) { Stop-Process -Id $gui.Id -Force } } catch {}
    try { if (-not $parent.HasExited) { Stop-Process -Id $parent.Id -Force } } catch {}
  }
}
"PASS: one-file distribution verified (cold and warm launch)" | Tee-Object -FilePath $log -Append
