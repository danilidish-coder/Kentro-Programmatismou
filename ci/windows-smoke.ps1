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
}
'@
$p = Start-Process -FilePath $exe -PassThru
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
    [UIMessage]::PostMessage($hwnd,0x0111,[IntPtr]110,[IntPtr]::Zero) | Out-Null
    Start-Sleep -Seconds 1
    $p.Refresh();if($p.HasExited){ throw 'FAIL: refreshing list crashed application' }
    Trace 'PASS: refresh action kept the process alive'
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
