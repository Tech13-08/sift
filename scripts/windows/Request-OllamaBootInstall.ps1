# Launches Install-OllamaBoot.ps1 elevated (UAC). Safe to run from WSL via make.
param(
    [string]$WslDistro = "Ubuntu"
)

$ErrorActionPreference = "Stop"
$unix = "/home/falak/repos/sift/scripts/windows/Install-OllamaBoot.ps1"
$src = ""
try { $src = (wsl.exe -d $WslDistro -- wslpath -w $unix).Trim() } catch { $src = "" }
if (-not $src) {
    $src = "\\wsl$\$WslDistro\home\falak\repos\sift\scripts\windows\Install-OllamaBoot.ps1"
}
if (-not (Test-Path -LiteralPath $src)) {
    throw "Cannot find Install-OllamaBoot.ps1 at $src"
}

$destDir = Join-Path $env:LOCALAPPDATA "Sift"
New-Item -ItemType Directory -Force -Path $destDir | Out-Null
$dest = Join-Path $destDir "Install-OllamaBoot.ps1"
Copy-Item -LiteralPath $src -Destination $dest -Force

Write-Host "Requesting Administrator approval (UAC) to register Ollama at boot..."
$p = Start-Process -FilePath "powershell.exe" `
    -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $dest) `
    -Verb RunAs `
    -Wait `
    -PassThru
if ($p.ExitCode -ne 0) {
    throw "Elevated install failed (exit $($p.ExitCode)). Approve the UAC prompt and retry."
}
Write-Host "Done. Verify: Get-ScheduledTask -TaskName 'Sift Start Ollama'"
