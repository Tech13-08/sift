# Ensures Ollama + Docker Desktop + k3d Sift cluster after PC boot.
# Called by the "Sift Ensure Up" scheduled task (see Install-SiftAutostart.ps1).
param(
    [string]$WslDistro = "Ubuntu",
    [string]$RepoUnix = "/home/falak/repos/sift",
    [int]$DockerWaitSeconds = 240
)

$ErrorActionPreference = "Continue"
$logDir = Join-Path $env:LOCALAPPDATA "Sift"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$log = Join-Path $logDir "ensure-up.log"

function Write-Log([string]$msg) {
    $line = "{0} {1}" -f (Get-Date -Format "yyyy-MM-ddTHH:mm:ss"), $msg
    Add-Content -Path $log -Value $line
    Write-Host $line
}

Write-Log "===== Ensure-SiftUp start ====="

# --- Ollama ---
$ollamaApp = Join-Path $env:LOCALAPPDATA "Programs\Ollama\ollama app.exe"
$ollamaCli = Join-Path $env:LOCALAPPDATA "Programs\Ollama\ollama.exe"
if (-not (Get-Process -Name "ollama app","ollama" -ErrorAction SilentlyContinue)) {
    if (Test-Path $ollamaApp) {
        Write-Log "starting Ollama app"
        Start-Process -FilePath $ollamaApp
    } elseif (Test-Path $ollamaCli) {
        Write-Log "starting ollama serve"
        Start-Process -FilePath $ollamaCli -ArgumentList "serve" -WindowStyle Hidden
    } else {
        Write-Log "WARN: Ollama not found under $env:LOCALAPPDATA\Programs\Ollama"
    }
} else {
    Write-Log "Ollama already running"
}

# --- Docker Desktop ---
$dockerExe = "C:\Program Files\Docker\Docker\Docker Desktop.exe"
if (-not (Get-Process -Name "Docker Desktop","com.docker.backend" -ErrorAction SilentlyContinue)) {
    if (Test-Path $dockerExe) {
        Write-Log "starting Docker Desktop"
        Start-Process -FilePath $dockerExe
    } else {
        Write-Log "ERROR: Docker Desktop not found"
        exit 1
    }
} else {
    Write-Log "Docker Desktop already running"
}

Write-Log "waiting for docker engine (up to ${DockerWaitSeconds}s)..."
$deadline = (Get-Date).AddSeconds($DockerWaitSeconds)
$dockerReady = $false
while ((Get-Date) -lt $deadline) {
    docker info 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) {
        $dockerReady = $true
        break
    }
    Start-Sleep -Seconds 5
}
if (-not $dockerReady) {
    Write-Log "ERROR: Docker engine not ready"
    exit 1
}
Write-Log "Docker engine OK"

# --- k3d / pods via WSL ---
$bashCmd = "bash -lc 'chmod +x `"$RepoUnix/scripts/ensure-sift-up.sh`" && `"$RepoUnix/scripts/ensure-sift-up.sh`"'"
Write-Log "running WSL heal: $bashCmd"
& wsl.exe -d $WslDistro -- bash -lc "chmod +x '$RepoUnix/scripts/ensure-sift-up.sh' && '$RepoUnix/scripts/ensure-sift-up.sh'"
$code = $LASTEXITCODE
Write-Log "WSL heal exit=$code"
Write-Log "===== Ensure-SiftUp done ====="
exit $code
