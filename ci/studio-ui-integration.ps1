$ErrorActionPreference = 'Stop'
$oldAppData = $env:LOCALAPPDATA
$exe = (Resolve-Path './publish/Kentro_Programmatismou.exe').Path
$fixture = (Get-Content './studio-module-fixture-location.txt' -Raw).Trim()
$log = Join-Path (Get-Location) 'studio-ui-integration.log'
if (-not (Test-Path $fixture)) { throw "Missing previous module integration fixture $fixture" }
$isolated = Join-Path $env:RUNNER_TEMP ('studio-native-gui-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $isolated | Out-Null
$env:LOCALAPPDATA = $isolated
$env:KENTRO_STUDIO_INTEGRATION_ZIP = $fixture
$env:KENTRO_STUDIO_INTEGRATION_RESULT = $log
$env:KENTRO_STUDIO_DIAG_DIR = (Resolve-Path '.').Path
$p = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe -Parent) -PassThru
try {
    if(-not $p.WaitForExit(720000)) {
        try { Stop-Process -Id $p.Id -Force } catch {}
        throw 'Timed out while exercising GUI + engine integration scenarios'
    }
    $p.Refresh()
    if (Test-Path $log) { Get-Content $log }
    if ($p.ExitCode -ne 0) { throw "Native GUI integration failed (exit code $($p.ExitCode))" }
    if(-not (Test-Path $log)) { throw 'Native GUI did not produce its integration report' }
    $line = Get-Content $log | Select-Object -Last 1
    if ($line -ne 'PASS: ALL NATIVE GUI + ENGINE MODULES INTEGRATED') {
        throw 'Native GUI did not verify all separate stages end-to-end'
    }
} finally {
    $env:LOCALAPPDATA = $oldAppData
    Remove-Item Env:KENTRO_STUDIO_INTEGRATION_ZIP -ErrorAction SilentlyContinue
    Remove-Item Env:KENTRO_STUDIO_INTEGRATION_RESULT -ErrorAction SilentlyContinue
}
