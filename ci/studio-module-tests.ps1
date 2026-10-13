# Real Windows integration contract for the independent local modules.
# Runs only against an isolated temporary LOCALAPPDATA; never against user data.
$ErrorActionPreference = 'Stop'
$oldAppData = $env:LOCALAPPDATA
$root = Join-Path $env:RUNNER_TEMP ('studio-module-e2e-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $root | Out-Null
$env:LOCALAPPDATA = $root
$bridge = (Resolve-Path './publish/Kentro_Engine.exe').Path
$report = Join-Path (Get-Location) 'studio-modules.log'
function Log([string]$s) { $s | Tee-Object -FilePath $report -Append }
function Assert([bool]$condition,[string]$message) { if(-not $condition) { throw "FAILED: $message" } }
function Bridge([string]$action, [string]$argument = '', [bool]$shouldFail = $false, [string]$extra = '') {
  if ($argument -ne '' -and $extra -ne '') { $raw = & $bridge $action $argument $extra }
  elseif ($argument -ne '') { $raw = & $bridge $action $argument }
  else { $raw = & $bridge $action }
  $code = $LASTEXITCODE
  $responses = @($raw | ForEach-Object { $_ | ConvertFrom-Json })
  $last = $responses | Select-Object -Last 1
  if ($shouldFail) {
    Assert ($code -ne 0 -and $last.type -eq 'error') "Expected guarded failure for $action"
    return $null
  }
  if($code -ne 0 -or $last.type -ne 'result') { throw "BRIDGE $action error code=${code}: $($last.message)" }
  return $last.payload
}
function WriteUtf8([string]$file,[string]$text) {
  $parent = Split-Path $file -Parent
  New-Item -ItemType Directory -Force -Path $parent | Out-Null
  [System.IO.File]::WriteAllText($file, $text, [System.Text.UTF8Encoding]::new($false))
}
try {
  "Isolated module integration suite — $(Get-Date -Format o)" | Set-Content $report
  $src = Join-Path $root 'fixture'
  WriteUtf8 (Join-Path $src 'go.mod') "module local.example/studiofixture`n`ngo 1.23`n"
  WriteUtf8 (Join-Path $src 'main.go') @'
package main
import "time"
func main() { time.Sleep(20 * time.Second) }
'@
  WriteUtf8 (Join-Path $src 'main_test.go') @'
package main
import "testing"
func TestReady(t *testing.T) { if 2 + 2 != 4 { t.Fatal("not ready") } }
'@
  WriteUtf8 (Join-Path $src 'setup/main.go') @'
package main
func main() {}
'@
  WriteUtf8 (Join-Path $src 'project-manifest.json') @'
{
  "name":"Studio Integration Fixture",
  "version":"1.0",
  "language":"Go",
  "tests":[{"name":"Unit and compatibility","command":"go","args":["test","-run","TestReady","./..."],"timeout_seconds":120}],
  "stress":[
    {"name":"Concurrent independent modules","kind":"parallel","command":"go","args":["test","-run","TestReady","./..."],"parallel":2,"repeats":2,"timeout_seconds":120},
    {"name":"Completed sequential heavy workload","kind":"sequential","module":"INTEGRATION","command":"go","args":["test","-run","TestReady","./..."],"parallel":1,"repeats":4,"timeout_seconds":300}
  ],
  "build":{"name":"Build Windows PE","command":"go","args":["build","-o","app.exe","."],"timeout_seconds":120},
  "smoke":{"name":"Start built executable","command":"./app.exe","expect_contains":"WINDOW_STAYS_OPEN","timeout_seconds":30},
  "setup":{"name":"Optional setup binary","command":"go","args":["build","-o","setup-installer.exe","./setup"],"timeout_seconds":120}
}
'@
  $zip = Join-Path $root 'input.zip'
  Compress-Archive -Path (Join-Path $src '*') -DestinationPath $zip
  [System.IO.File]::WriteAllText((Join-Path (Get-Location) 'studio-module-fixture-location.txt'), $zip, [System.Text.UTF8Encoding]::new($false))
  $initial = @(Bridge 'list')
  Assert ($initial.Count -eq 0) 'Initial isolated project list must be empty'
  Log 'PASS: local isolation and empty list'
  $preview = Bridge 'preview' $zip
  Assert ($preview.Files -ge 5) 'ZIP preview must count real files'
  Log 'PASS: ZIP preview'
  $project = Bridge 'import' $zip
  $id = [string]$project.id
  Assert ($id.Length -ge 8) 'Imported project ID'
  $listed = @(Bridge 'list')
  Assert ($listed.Count -eq 1) 'Project list after import'
  $detail = Bridge 'details' $id
  Assert ($detail.project.id -eq $id -and $detail.manifest.tests.Count -eq 1 -and $detail.manifest.stress.Count -eq 2) 'Manifest/identity in details'
  Assert (-not $detail.ready_build -and -not $detail.ready_setup) 'Build and setup must be initially locked'
  Log 'PASS: ZIP import, project list, manifest parsing and disabled gates'
  $null = Bridge 'build' $id $true
  $null = Bridge 'setup' $id $true
  Log 'PASS: cannot bypass tests and setup gates'
  $checks = Bridge 'checks' $id
  Assert ($checks.stage -eq 'CHECKS' -and -not $checks.blocked) 'Complete checks stage must pass'
  Assert (@($checks.results | Where-Object {$_.phase -eq 'TEST' -and $_.status -eq 'PASS'}).Count -ge 1) 'Positive unit tests'
  Assert (@($checks.results | Where-Object {$_.phase -eq 'STRESS' -and $_.status -eq 'PASS'}).Count -ge 2) 'Parallel and sequential stress tests'
  Assert (@($checks.results | Where-Object {$_.phase -eq 'STRESS' -and $_.reason -like '*διαδοχικές*'}).Count -eq 1) 'Sequential workload completes without killing test subprocesses'
  $afterChecks = Bridge 'details' $id
  Assert (-not $afterChecks.ready_build -and -not $afterChecks.ready_setup -and
          $afterChecks.review_status.status -eq 'WAITING_REVIEW') 'Build must await human review'
  $null = Bridge 'build' $id $true
  Log 'PASS: tests pass but EXE remains locked until explicit review'

  # Only a synthetic CI fixture receives a simulated review receipt here.
  $request = Bridge 'review-request' $id
  $decision = @{
    schema = $request.schema
    decision = 'APPROVED_FOR_BUILD'
    project_id = $request.project_id
    run_id = $request.run_id
    source_sha256 = $request.source_sha256
    plan_sha256 = $request.plan_sha256
    results_sha256 = $request.results_sha256
    reviewer = 'ChatGPT'
    note = 'CI simulated review of synthetic fixture only'
  }
  $reviewFile = Join-Path $root 'ci-review.json'
  WriteUtf8 $reviewFile ($decision | ConvertTo-Json -Depth 4)
  $review = Bridge 'review-import' $id $false $reviewFile
  Assert ($review.status -eq 'APPROVED') 'Synthetic fixture review receipt import failed'
  $afterReview = Bridge 'details' $id
  Assert ($afterReview.ready_build -and -not $afterReview.ready_setup) 'Only EXE unlocked after matching manual review'
  Log 'PASS: external review receipt binds to exact checks, code and plan'
  $built = Bridge 'build' $id
  Assert ($built.approved -eq $true -and -not $built.run.blocked) 'Build approval'
  Assert ($built.run.stage -eq 'BUILD') 'Separated BUILD stage'
  $stable = [string]$built.stable_folder
  Assert (Test-Path (Join-Path $stable 'artifacts\app.exe')) 'Real approved executable artifact'
  Assert (@($built.run.results | Where-Object {$_.phase -eq 'SMOKE' -and $_.status -eq 'PASS'}).Count -ge 1) 'Real built EXE startup'
  $afterBuild = Bridge 'details' $id
  Assert ($afterBuild.ready_setup) 'Setup unlocked only after verified EXE'
  Log 'PASS: PE build, actual startup, approval/hash gate and unlock setup'
  $setup = Bridge 'setup' $id
  Assert ($setup.stage -eq 'SETUP' -and -not $setup.blocked) 'Independent optional SETUP pass'
  Assert (@($setup.artifacts | Where-Object {$_.name -like '*setup-installer.exe'}).Count -ge 1) 'Setup artifact'
  Assert (Test-Path (Join-Path $stable 'artifacts\app.exe')) 'Setup must not overwrite approved EXE'
  Log 'PASS: independent setup and approved exe preserved'
  $summary = Bridge 'report' $id
  Assert (Test-Path $summary.path) 'Persistent report ZIP'
  $backup = Bridge 'backup' $id
  Assert (Test-Path (Join-Path $backup.path 'backup.json')) 'SHA256 backup metadata'
  Log 'PASS: reports and verified backup'
  $folder = Bridge 'folder' $id
  Assert (Test-Path (Join-Path $folder.path 'project.json')) 'Active project folder'
  $removed = Bridge 'trash' $id
  Assert ($removed.name -eq $project.name) 'Soft delete responds with project name'
  Assert (@(Bridge 'list').Count -eq 0) 'Trashed project invisible in active list'
  $restored = Bridge 'restore'
  Assert ($restored.id -eq $id) 'Restored project identity unchanged'
  Assert (@(Bridge 'list').Count -eq 1) 'Restored project available'
  Assert (Test-Path (Join-Path $stable 'artifacts\app.exe')) 'Restoration preserves approved EXE'
  Log 'PASS: project folder, reversible deletion, restoration, original exe integrity'
  $bad = Join-Path $root 'bad.zip'
  WriteUtf8 (Join-Path $root 'bad-entry.txt') 'not a valid ZIP'
  Copy-Item (Join-Path $root 'bad-entry.txt') $bad
  $null = Bridge 'preview' $bad $true
  $null = Bridge 'import' $bad $true
  Assert (@(Bridge 'list').Count -eq 1) 'Invalid input must not mutate project registry'
  Log 'PASS: malformed ZIP fails closed without mutation'
  Log 'PASS: ALL MODULES INTEGRATED — import/checks/stress/EXE/smoke/setup/report/backup/trash/restore'
} finally {
  $env:LOCALAPPDATA = $oldAppData
}
