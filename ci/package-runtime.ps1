$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
function CopyAll([string]$from,[string]$to) {
 if(-not(Test-Path $from)){throw "Missing toolchain directory: $from"}
 New-Item -ItemType Directory -Path $to -Force | Out-Null
 & robocopy.exe $from $to /E /R:1 /W:1 /NP /NFL /NDL /NJH /NJS /XF "*.pdb"
 if($LASTEXITCODE -gt 7){throw "robocopy failed $from -> $to : $LASTEXITCODE"}
}
$root=Join-Path (Resolve-Path '.') 'portable-toolchains-staging'
$out=Join-Path (Resolve-Path '.') 'Studio/Assets/toolchains.zip'
if(Test-Path $root){Remove-Item $root -Recurse -Force}
New-Item -ItemType Directory $root -Force | Out-Null
New-Item -ItemType Directory (Split-Path $out) -Force | Out-Null
$pyHome=Split-Path (Get-Command python).Source -Parent
$goHome=(& go env GOROOT).Trim()
$dotnetHome=Split-Path (Get-Command dotnet).Source -Parent
$nodeHome=Split-Path (Get-Command node).Source -Parent
("PYTHON="+$pyHome+"; GO="+$goHome+"; DOTNET="+$dotnetHome+"; NODE="+$nodeHome) | Set-Content runtime-package.log
CopyAll $pyHome (Join-Path $root 'python')
CopyAll $goHome (Join-Path $root 'go')
# Node.js runtime: node.exe is enough for local node tests; npm packages are project-local.
New-Item -ItemType Directory (Join-Path $root 'node') -Force | Out-Null
Copy-Item (Join-Path $nodeHome 'node.exe') (Join-Path $root 'node/node.exe')
# Copy only the selected .NET 8 SDK and matching local references/runtime.
$dest=Join-Path $root 'dotnet'
New-Item -ItemType Directory $dest -Force | Out-Null
Copy-Item (Join-Path $dotnetHome 'dotnet.exe') $dest
foreach($d in @('host','metadata')) {
 if(Test-Path (Join-Path $dotnetHome $d)){CopyAll (Join-Path $dotnetHome $d) (Join-Path $dest $d)}
}
foreach($part in @(
 'sdk/8.0.*',
 'shared/Microsoft.NETCore.App/8.0.*',
 'shared/Microsoft.WindowsDesktop.App/8.0.*',
 'packs/Microsoft.NETCore.App.Ref/8.0.*',
 'packs/Microsoft.WindowsDesktop.App.Ref/8.0.*',
 'packs/Microsoft.AspNetCore.App.Ref/8.0.*',
 'packs/NETStandard.Library.Ref/*'
)){
 $items=Get-Item (Join-Path $dotnetHome $part) -ErrorAction SilentlyContinue
 foreach($item in ($items | Sort-Object -Property @{Expression={[version]$_.Name}} -Descending | Select-Object -First 1)){
  $rel=$item.FullName.Substring($dotnetHome.Length).TrimStart('\','/')
  CopyAll $item.FullName (Join-Path $dest $rel)
 }
}
foreach($path in @('python/python.exe','go/bin/go.exe','dotnet/dotnet.exe','node/node.exe')){
 if(-not(Test-Path (Join-Path $root $path))){throw "Portable runtime missing $path"}
}
Write-Host 'Creating compressed portable toolchains resource.'
if(Test-Path $out){Remove-Item $out -Force}
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
[System.IO.Compression.ZipFile]::CreateFromDirectory($root,$out,[System.IO.Compression.CompressionLevel]::Optimal,$false)
$bytes=(Get-Item $out).Length
("RUNTIME_ZIP_BYTES="+$bytes) | Add-Content runtime-package.log
Get-ChildItem $root -Directory | ForEach-Object {($_.Name+"="+((Get-ChildItem $_.FullName -Recurse -File | Measure-Object Length -Sum).Sum)) | Add-Content runtime-package.log}
if($bytes -lt 80000000 -or $bytes -gt 2030000000){throw "Unexpected runtime archive size: $bytes"}
Get-Content runtime-package.log
# Release transient staging space before compiling and running acceptance tests.
Remove-Item $root -Recurse -Force
# robocopy uses nonzero success codes; never propagate a successful copy as a job failure.
exit 0
