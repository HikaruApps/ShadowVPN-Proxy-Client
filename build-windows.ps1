$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$projectRoot = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd('\')
$blockingProcesses = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
    $processName = $_.Name.ToLowerInvariant()
    $isShadowVpnProcess = $processName -in @('shadowvpn.exe', 'shadowvpn-core.exe')
    $isBuildProcess = $processName -in @('node.exe', 'cargo.exe')
    $belongsToProject = ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($projectRoot, [StringComparison]::OrdinalIgnoreCase)) -or
        ($_.CommandLine -and $_.CommandLine.IndexOf($projectRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0)
    $isShadowVpnProcess -or ($isBuildProcess -and $belongsToProject)
})
if ($blockingProcesses.Count -gt 0) {
    $processList = ($blockingProcesses | ForEach-Object { '{0} (PID {1})' -f $_.Name, $_.ProcessId }) -join ', '
    throw "ShadowVPN is still running: $processList. Close it or run .\stop-shadowvpn.cmd, then build again."
}
foreach ($tool in @('go', 'cargo', 'rustc', 'npm')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "Missing $tool. Install Go 1.27+, Rust MSVC, Node.js LTS, and Visual Studio C++ Build Tools." }
}
$hostTriple = (& rustc --print host-tuple).Trim()
if ($hostTriple -ne 'x86_64-pc-windows-msvc') { throw "Windows x64 MSVC Rust is required. Current host: $hostTriple" }
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    Push-Location backend
    try {
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
        go build -buildvcs=false -trimpath -ldflags '-s -w' -o ../bin/shadowvpn-core.exe .
        if ($LASTEXITCODE -ne 0) { throw 'Go backend build failed.' }
    } finally { Pop-Location }
} finally {
    $env:GOOS = $previousGOOS; $env:GOARCH = $previousGOARCH; $env:CGO_ENABLED = $previousCGO
}
if (-not (Test-Path 'bin\wintun.dll')) { throw 'bin\wintun.dll is missing. Restore it from the project archive.' }
node scripts/test-bridge.cjs
if ($LASTEXITCODE -ne 0) { throw 'Frontend bridge test failed.' }
node scripts/test-renderer-helpers.cjs
if ($LASTEXITCODE -ne 0) { throw 'Frontend renderer helper test failed.' }
if (-not (Test-Path 'node_modules\@tauri-apps\cli')) {
    npm.cmd ci
    if ($LASTEXITCODE -ne 0) { throw 'npm install failed.' }
} else {
    Write-Host 'npm dependencies are already installed.'
}
npm.cmd run build
if ($LASTEXITCODE -ne 0) { throw 'Tauri build failed.' }
Write-Host 'Done. Installer: src-tauri\target\release\bundle\nsis\'
