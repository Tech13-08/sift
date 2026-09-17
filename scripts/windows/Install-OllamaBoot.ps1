# Registers "Sift Start Ollama" at Windows boot (no interactive sign-in required).
# Must run elevated (UAC). Prefer:
#   make install-ollama-autostart
#
# Uninstall (elevated):
#   Unregister-ScheduledTask -TaskName 'Sift Start Ollama' -Confirm:$false

param(
    [string]$TaskName = "Sift Start Ollama",
    [int]$DelayMinutes = 1
)

$ErrorActionPreference = "Stop"

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run elevated (Administrator). From WSL: make install-ollama-autostart"
}

$ollamaApp = Join-Path $env:LOCALAPPDATA "Programs\Ollama\ollama app.exe"
$ollamaCli = Join-Path $env:LOCALAPPDATA "Programs\Ollama\ollama.exe"
if (Test-Path $ollamaApp) {
    $action = New-ScheduledTaskAction -Execute $ollamaApp
    $exeNote = $ollamaApp
} elseif (Test-Path $ollamaCli) {
    $action = New-ScheduledTaskAction -Execute $ollamaCli -Argument "serve"
    $exeNote = "$ollamaCli serve"
} else {
    throw "Ollama not found under $($env:LOCALAPPDATA)\Programs\Ollama"
}

$trigger = New-ScheduledTaskTrigger -AtStartup
$trigger.Delay = "PT${DelayMinutes}M"

$taskPrincipal = New-ScheduledTaskPrincipal `
    -UserId $env:USERNAME `
    -LogonType S4U `
    -RunLevel Highest

$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -ExecutionTimeLimit (New-TimeSpan -Hours 0) `
    -RestartCount 5 `
    -RestartInterval (New-TimeSpan -Minutes 1)

if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger $trigger `
    -Principal $taskPrincipal `
    -Settings $settings `
    -Description "Start Ollama at PC boot so Sift digests work before Windows sign-in." | Out-Null

Write-Host "Registered '$TaskName' (AtStartup + ${DelayMinutes}m, S4U/Highest)."
Write-Host "Starts: $exeNote"
Write-Host "Test: Start-ScheduledTask -TaskName '$TaskName'"
