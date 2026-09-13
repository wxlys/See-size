#Requires -Version 7.0
param([string]$SshHost = '18', [int]$LocalPort = 18082)
$ErrorActionPreference = 'Stop'
if ($LocalPort -lt 1024 -or $LocalPort -gt 65535) { throw 'Invalid local port' }
Write-Host "SeeSize preview: http://127.0.0.1:$LocalPort — keep this terminal open; Ctrl+C to stop."
while ($true) {
    & ssh -N -L "127.0.0.1:${LocalPort}:127.0.0.1:18080" -o BatchMode=yes -o ExitOnForwardFailure=yes -o ConnectTimeout=10 -o ServerAliveInterval=30 -o ServerAliveCountMax=3 $SshHost
    if ($LASTEXITCODE -eq 0) { break }
    if (Get-NetTCPConnection -LocalPort $LocalPort -State Listen -ErrorAction SilentlyContinue) {
        throw 'Local port is already in use. Check the existing preview before starting another.'
    }
    Write-Warning 'SSH disconnected; retrying in 5 seconds.'
    Start-Sleep -Seconds 5
}
