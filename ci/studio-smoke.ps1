$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$exe = Join-Path $PSScriptRoot '..\publish\Kentro_Programmatismou.exe'
$log = Join-Path $PSScriptRoot '..\studio-smoke.log'
"Opening native WinUI 3 desktop application..." | Set-Content $log
$p = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe) -PassThru
try {
  $handle = [IntPtr]::Zero
  for ($i=0; $i -lt 40; $i++) {
    Start-Sleep -Milliseconds 500
    $p.Refresh()
    if ($p.HasExited) { throw "FAIL: new UI process exited with code $($p.ExitCode)" }
    if ($p.MainWindowHandle -ne [IntPtr]::Zero) { $handle = $p.MainWindowHandle; break }
  }
  if ($handle -eq [IntPtr]::Zero) { throw "NOT VERIFIED: no interactive native Windows window available" }
  "PASS: WinUI 3 window hwnd=$handle" | Tee-Object -FilePath $log -Append
  Start-Sleep -Seconds 2
  if ($p.HasExited) { throw "FAIL: UI crashed after startup" }
  Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class Bounds {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out Rect r);
  [StructLayout(LayoutKind.Sequential)] public struct Rect { public int L, T, R, B; }
}
'@
  $r = [Bounds+Rect]::new()
  if(-not [Bounds]::GetWindowRect($handle, [ref]$r)) { throw 'Could not inspect GUI rectangle' }
  $w=$r.R-$r.L; $h=$r.B-$r.T
  if($w -lt 600 -or $h -lt 400){throw "GUI too small: $w x $h"}
  $screen=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds
  if($r.L -lt $screen.Left -or $r.T -lt $screen.Top -or $r.R -gt $screen.Right -or $r.B -gt $screen.Bottom) {
     throw "GUI exceeds visible desktop: $w x $h on $($screen.Width) x $($screen.Height)"
  }
  $bmp = New-Object System.Drawing.Bitmap($w,$h)
  $g=[System.Drawing.Graphics]::FromImage($bmp)
  try {
    $g.CopyFromScreen($r.L,$r.T,0,0,$bmp.Size)
    $bmp.Save((Join-Path $PSScriptRoot '..\studio-screenshot.png'))
  } finally { $g.Dispose(); $bmp.Dispose() }
  "PASS: captured native GUI at $w x $h" | Tee-Object -FilePath $log -Append
} finally {
  try { if(-not $p.HasExited) { Stop-Process -Id $p.Id -Force } } catch {}
}
