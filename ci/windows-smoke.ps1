$ErrorActionPreference = 'Stop'
$exe = Join-Path $PSScriptRoot '..\build\Kentro_Programmatismou.exe'
$log = Join-Path $PSScriptRoot '..\build\windows-ui-smoke.log'
function Trace($s) { $s | Out-File -Append -FilePath $log -Encoding utf8; Write-Host $s }
Trace 'Windows native UI smoke: start'
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class UIMessage {
  [DllImport("user32.dll", SetLastError=true)] public static extern bool PostMessage(IntPtr handle, uint message, IntPtr wp, IntPtr lp);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindowEx(IntPtr parent, IntPtr childAfter, string className, string title);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowTextLength(IntPtr hwnd);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr hwnd, System.Text.StringBuilder buffer, int maxCount);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hwnd, out Rect rect);
  [DllImport("user32.dll")] public static extern int GetDlgCtrlID(IntPtr hwnd);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessage(IntPtr hwnd, uint message, IntPtr wp, System.Text.StringBuilder lp);
  [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr hwnd, uint message, IntPtr wp, IntPtr lp);
  [StructLayout(LayoutKind.Sequential)] public struct Rect { public int L,T,R,B; }

}
'@
$env:KENTRO_UI_SMOKE_TEST = "1"
$p = Start-Process -FilePath $exe -PassThru
Remove-Item Env:KENTRO_UI_SMOKE_TEST -ErrorAction SilentlyContinue
try {
    $hwnd=[IntPtr]::Zero
    for ($i=0;$i -lt 50;$i++) {
        Start-Sleep -Milliseconds 300
        $p.Refresh()
        if ($p.HasExited) { throw "FAIL: Application crashed immediately. ExitCode: $($p.ExitCode)" }
        if ($p.MainWindowHandle -ne [IntPtr]::Zero) { $hwnd=$p.MainWindowHandle; break }
    }
    if ($hwnd -eq [IntPtr]::Zero) { throw 'NOT RUN: Windows runner does not expose a usable desktop session' }
    Trace "PASS: visible native main window: $hwnd"
    [UIMessage]::PostMessage($hwnd, 0x8009, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null
    Start-Sleep -Milliseconds 1300
    $results=[IntPtr]::Zero
    $after=[IntPtr]::Zero
    while($true){
      $next=[UIMessage]::FindWindowEx($hwnd,$after,'ListBox',$null)
      if($next -eq [IntPtr]::Zero){break}
      if([UIMessage]::GetDlgCtrlID($next) -eq 91){$results=$next;break}
      $after=$next
    }
    if($results -eq [IntPtr]::Zero){throw 'FAIL: no independent results list control found'}
    $count=[int][UIMessage]::SendMessage($results,0x18B,[IntPtr]::Zero,[IntPtr]::Zero)
    if($count -lt 35){ throw "FAIL: missing distinct result rows (count=$count)" }
    $last=[System.Text.StringBuilder]::new(256)
    [void][UIMessage]::SendMessage($results,0x189,[IntPtr]($count-1),$last)
    if($last.ToString() -notmatch '34'){throw 'FAIL: wrong text in final distinct result row'}
    Trace "PASS: $count independent result rows, last row correct (no overprinting)"
    try {
      Add-Type -AssemblyName System.Drawing
      $rc = [UIMessage+Rect]::new()
      if([UIMessage]::GetWindowRect($hwnd,[ref]$rc)) {
        $width = [Math]::Max(1,$rc.R-$rc.L); $height = [Math]::Max(1,$rc.B-$rc.T)
        $bmp = [System.Drawing.Bitmap]::new($width,$height)
        $graphics = [System.Drawing.Graphics]::FromImage($bmp)
        try {$graphics.CopyFromScreen($rc.L,$rc.T,0,0,[System.Drawing.Size]::new($width,$height))
          $bmp.Save((Join-Path $PSScriptRoot '..\build\windows-gui-render.png'),[System.Drawing.Imaging.ImageFormat]::Png)
          Trace 'PASS: captured UI rendering for manual visual review'
        } finally {$graphics.Dispose();$bmp.Dispose()}
      }
    } catch { Trace "NOT RUN: screenshot unsupported on runner: $($_.Exception.Message)" }

    [UIMessage]::PostMessage($hwnd,0x0111,[IntPtr]101,[IntPtr]::Zero) | Out-Null
    $picker = $null
    for ($i=0;$i -lt 50;$i++) {
        Start-Sleep -Milliseconds 300
        $p.Refresh();if($p.HasExited){throw 'FAIL: ZIP import click terminated the application'}
        $picker = Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.ParentProcessId -eq $p.Id } | Select-Object -First 1
        if($picker){ break }
    }
    if(-not $picker){throw 'FAIL: ZIP click did not start isolated Windows picker'}
    Trace "PASS: ZIP button spawned isolated dialog process PID $($picker.ProcessId)"
    Stop-Process -Id $picker.ProcessId -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
    $p.Refresh();if($p.HasExited){throw 'FAIL: failed/cancelled chooser crashed application'}
    Trace 'PASS: picker termination did not close application'
} finally {
    if(-not $p.HasExited){ Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue }
}
Trace 'PASS: Windows smoke completed'
