$ErrorActionPreference = 'Stop'
$oldAppData = $env:LOCALAPPDATA
$root = Join-Path $env:RUNNER_TEMP ('wpf-ui-integration-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $root | Out-Null
$exe = (Resolve-Path './publish/Kentro_Programmatismou.exe').Path
$fixture = (Get-Content './studio-module-fixture-location.txt' -Raw).Trim()
if(-not (Test-Path $fixture)) { throw "Missing ZIP fixture: $fixture" }
$log = Join-Path (Get-Location) 'wpf-ui-integration.log'
$env:LOCALAPPDATA = $root
$env:KENTRO_WPF_INTEGRATION_ZIP = $fixture
$env:KENTRO_WPF_INTEGRATION_RESULT = $log
$env:KENTRO_WPF_SCREENSHOT = Join-Path (Get-Location) 'wpf-screenshot.png'
$env:KENTRO_STUDIO_DIAG_DIR = (Resolve-Path '.').Path
try {
  $process = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe -Parent) -PassThru
  try {
    if(-not $process.WaitForExit(720000)) {
      Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
      throw 'WPF UI integration timed out'
    }
    $process.Refresh()
    if(Test-Path $log) { Get-Content $log }
    if($process.ExitCode -ne 0) { throw "WPF integrated GUI returned $($process.ExitCode)" }
    if(-not (Test-Path $log)) { throw 'WPF GUI did not write its integration log' }
    $lastLine = Get-Content $log | Select-Object -Last 1
    if($lastLine -ne 'PASS: ALL WPF GUI + GO ENGINE MODULES INTEGRATED') {
      throw 'Not all WPF GUI stages passed'
    }
    if(-not (Test-Path './wpf-screenshot.png')) { throw 'WPF screenshot missing' }
  } finally {
    $process.Refresh()
    if(-not $process.HasExited) { Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue }
  }
} finally {
  $env:LOCALAPPDATA = $oldAppData
  Remove-Item Env:KENTRO_WPF_INTEGRATION_ZIP -ErrorAction SilentlyContinue
  Remove-Item Env:KENTRO_WPF_INTEGRATION_RESULT -ErrorAction SilentlyContinue
  Remove-Item Env:KENTRO_WPF_SCREENSHOT -ErrorAction SilentlyContinue
}
