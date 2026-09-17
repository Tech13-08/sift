# Registers Windows autostart so Sift comes back after reboot.
# Copies Ensure-SiftUp.ps1 to %LOCALAPPDATA%\Sift\ 
#
# Prefer: AtLogOn task (no admin). If you want pre-login boot heal, re-run elevated:
#   powershell -ExecutionPolicy Bypass -File ...\Install-SiftAutostart.ps1 -PreferBoot
#
# Uninstall:
#   Unregister-ScheduledTask -TaskName 'Sift Ensure Up' -Confirm:$false
#   Remove-Item "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup\SiftEnsureUp.cmd" -ErrorAction SilentlyContinue

param(
    [string]$TaskName = "Sift Ensure Up",
    [string]$WslDistro = "Ubuntu",
    [int]$DelayMinutes = 2,
    [switch]$PreferBoot
)

$ErrorActionPreference = "Stop"

$srcUnix = "/home/falak/repos/sift/scripts/windows/Ensure-SiftUp.ps1"
$src = ""
try {
    $src = (wsl.exe -d $WslDistro -- wslpath -w $srcUnix).Trim()
} catch {
    $src = ""
}
if (-not $src) {
    $src = "\\wsl$\$WslDistro\home\falak\repos\sift\scripts\windows\Ensure-SiftUp.ps1"
}
if (-not (Test-Path -LiteralPath $src)) {
    throw "Cannot find Ensure-SiftUp.ps1 at $src. Expected repo at /home/falak/repos/sift in $WslDistro."
}

$destDir = Join-Path $env:LOCALAPPDATA "Sift"
New-Item -ItemType Directory -Force -Path $destDir | Out-Null
$dest = Join-Path $destDir "Ensure-SiftUp.ps1"
Copy-Item -LiteralPath $src -Destination $dest -Force

$arg = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$dest`""
$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $arg

$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -ExecutionTimeLimit (New-TimeSpan -Hours 1) `
    -RestartCount 3 `
    -RestartInterval (New-TimeSpan -Minutes 2)

function Register-SiftTask {
    param($Trigger, $Principal, $ModeLabel)
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    }
    Register-ScheduledTask `
        -TaskName $TaskName `
        -Action $action `
        -Trigger $Trigger `
        -Principal $Principal `
        -Settings $settings `
        -Description "Start Ollama, wait for Docker, k3d cluster start and wait for Sift pods." | Out-Null
    Write-Host "Registered '$TaskName' ($ModeLabel)."
}

$registered = $false
if ($PreferBoot) {
    try {
        $trigger = New-ScheduledTaskTrigger -AtStartup
        $trigger.Delay = "PT${DelayMinutes}M"
        $principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType S4U -RunLevel Highest
        Register-SiftTask -Trigger $trigger -Principal $principal -ModeLabel "AtStartup + ${DelayMinutes}m (S4U/Highest)"
        $registered = $true
    } catch {
        Write-Host "WARN: PreferBoot failed ($($_.Exception.Message)). Falling back to AtLogOn."
    }
}

if (-not $registered) {
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
    try { $trigger.Delay = "PT${DelayMinutes}M" } catch { }
    $principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited
    Register-SiftTask -Trigger $trigger -Principal $principal -ModeLabel "AtLogOn + ${DelayMinutes}m"
}

$startupDir = [Environment]::GetFolderPath("Startup")
$cmdPath = Join-Path $startupDir "SiftEnsureUp.cmd"
@"
@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$dest"
"@ | Set-Content -Path $cmdPath -Encoding ASCII
Write-Host "Wrote Startup shortcut: $cmdPath"

Write-Host "Script: $dest"
Write-Host "Log:    $(Join-Path $destDir 'ensure-up.log')"
Write-Host "WSL log: \\wsl$\$WslDistro\home\falak\.local\state\sift\ensure-up.log"
Write-Host ""
Write-Host "Test now:"
Write-Host "  Start-ScheduledTask -TaskName '$TaskName'"
Write-Host "For heal before Windows sign-in, re-run elevated with -PreferBoot."
