$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $elevated = Start-Process powershell.exe -Verb RunAs -Wait -PassThru -ArgumentList ('-NoProfile -ExecutionPolicy Bypass -File "{0}"' -f $PSCommandPath)
    exit $elevated.ExitCode
}

$projectRoot = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd('\')
$processes = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
    $processName = $_.Name.ToLowerInvariant()
    $isShadowVpnProcess = $processName -in @('shadowvpn.exe', 'shadowvpn-core.exe', 'node.exe', 'cargo.exe')
    $belongsToProject = ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($projectRoot, [StringComparison]::OrdinalIgnoreCase)) -or
        ($_.CommandLine -and $_.CommandLine.IndexOf($projectRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0)
    $isShadowVpnProcess -and $belongsToProject
})

if ($processes.Count -eq 0) {
    Write-Host 'ShadowVPN is not running.'
} else {
    foreach ($process in $processes) {
        Write-Host ('Stopping {0} (PID {1})...' -f $process.Name, $process.ProcessId)
        Stop-Process -Id $process.ProcessId -Force -ErrorAction Stop
    }
    Write-Host 'ShadowVPN processes stopped. Locked files can now be replaced.'
}

$killSwitchAdapter = Get-NetAdapter -Name 'ShadowVPN Kill Switch' -ErrorAction SilentlyContinue
if ($killSwitchAdapter) {
    Get-NetRoute -InterfaceIndex $killSwitchAdapter.ifIndex -ErrorAction SilentlyContinue |
        Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
    Get-NetIPAddress -InterfaceIndex $killSwitchAdapter.ifIndex -ErrorAction SilentlyContinue |
        Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue
    Write-Host 'Legacy ShadowVPN Kill Switch routes cleared.'
}
