param([string]$Version)

$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')).TrimEnd('\')
if (-not $Version) {
    $package = Get-Content -LiteralPath (Join-Path $projectRoot 'package.json') -Raw | ConvertFrom-Json
    $Version = [string]$package.version
}
if ($Version -notmatch '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
    throw "Invalid application version: $Version"
}

$installerName = "ShadowVPN_${Version}_x64-setup.exe"
$bundleDirectory = Join-Path $projectRoot 'src-tauri\target\release\bundle\nsis'
$installerPath = Join-Path $bundleDirectory $installerName
$signaturePath = "$installerPath.sig"
if (-not (Test-Path -LiteralPath $installerPath)) { throw "Updater installer not found: $installerPath" }
if (-not (Test-Path -LiteralPath $signaturePath)) { throw "Updater signature not found: $signaturePath" }

$signature = (Get-Content -LiteralPath $signaturePath -Raw).Trim()
if (-not $signature) { throw 'Updater signature is empty.' }
$downloadUrl = "https://github.com/HikaruApps/ShadowVPN-Proxy-Client/releases/download/v$Version/$installerName"
$manifest = [ordered]@{
    version = $Version
    notes = "ShadowVPN Desktop v$Version"
    pub_date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    platforms = [ordered]@{
        'windows-x86_64-nsis' = [ordered]@{
            signature = $signature
            url = $downloadUrl
        }
    }
}

$updatesDirectory = Join-Path $projectRoot 'updates'
New-Item -ItemType Directory -Force -Path $updatesDirectory | Out-Null
$manifestPath = Join-Path $updatesDirectory 'latest.json'
$json = $manifest | ConvertTo-Json -Depth 5
[IO.File]::WriteAllText($manifestPath, "$json`n", [Text.UTF8Encoding]::new($false))
Write-Host "Updater manifest: $manifestPath"
