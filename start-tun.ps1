$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$release = Join-Path $PSScriptRoot 'src-tauri\target\release\shadowvpn.exe'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $elevated = Start-Process powershell.exe -Verb RunAs -Wait -PassThru -ArgumentList ('-NoProfile -ExecutionPolicy Bypass -File "{0}"' -f $PSCommandPath)
    exit $elevated.ExitCode
}
$expectedVersion = (Get-Content (Join-Path $PSScriptRoot 'package.json') -Raw | ConvertFrom-Json).version
if (Test-Path $release) {
    $releaseVersion = (Get-Item $release).VersionInfo.ProductVersion
    if ($releaseVersion -and $releaseVersion.StartsWith($expectedVersion, [StringComparison]::OrdinalIgnoreCase)) {
        & $release
        exit
    }
    Write-Host "Ignoring stale release $releaseVersion; project version is $expectedVersion."
}
if (-not (Get-Command cargo -ErrorAction SilentlyContinue)) {
    Write-Host 'Install Rust (MSVC) and Visual Studio C++ Build Tools, then reopen your terminal.'
    Read-Host 'Press Enter'
    exit 1
}
if (-not (Test-Path 'node_modules\@tauri-apps\cli')) {
    Write-Host 'Run npm.cmd ci in the project folder first.'
    Read-Host 'Press Enter'
    exit 1
}
npm.cmd start
if ($LASTEXITCODE -ne 0) { Read-Host 'Launch failed. Press Enter' }
