param([Parameter(Mandatory)][string]$Go)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$stage = Join-Path ([IO.Path]::GetTempPath()) ('seesize-lab-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $stage | Out-Null
Push-Location $repo
$oldOS = $env:GOOS; $oldArch = $env:GOARCH; $oldCGO = $env:CGO_ENABLED
$oldToken = $env:SEE_SIZE_AGENT_TOKEN
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    foreach ($component in @('hub','agent')) {
        $exe = Join-Path $stage "$component.exe"
        & $Go build -o $exe "./cmd/$component"
        if ($LASTEXITCODE -ne 0) { throw 'Native build failed' }
        $env:SEE_SIZE_AGENT_TOKEN = 'seesize-test-secret-sentinel'
        foreach ($argument in @('-h','-unknown-test-flag')) {
            $output = (& $exe $argument 2>&1 | Out-String)
            $exitCode = $LASTEXITCODE
            if ($output.Contains($env:SEE_SIZE_AGENT_TOKEN)) { throw 'Credential leaked in usage' }
            if (($argument -eq '-h' -and $exitCode -ne 0) -or ($argument -ne '-h' -and $exitCode -ne 2)) { throw 'Unexpected CLI exit code' }
            if ($component -eq 'hub' -and -not $output.Contains('-backup-dir')) { throw 'Missing backup-dir capability' }
        }
    }
    $env:GOOS = 'linux'
    $hashes = @{}
    foreach ($component in @('hub','agent','backup')) {
        $name = "seesize-$component-linux-amd64"
        & $Go build -trimpath -o (Join-Path $stage $name) "./cmd/$component"
        if ($LASTEXITCODE -ne 0) { throw 'Linux build failed' }
        $hashes[$name] = (Get-FileHash (Join-Path $stage $name) -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    @{commit=(& git rev-parse HEAD); working_tree=(& git status --porcelain | Out-String); sha256=$hashes} | ConvertTo-Json | Set-Content (Join-Path $stage 'build-manifest.json')
    $files = @('setup-upgrade-lab.py','upgrade-prepare.py','storage-maintenance.py')
    foreach ($file in $files) { Copy-Item (Join-Path $PSScriptRoot $file) $stage }
    $archive = Join-Path ([Environment]::GetFolderPath('Desktop')) ('seesize-upgrade-lab-fixed-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '.zip')
    $items = @((Join-Path $stage '*linux-amd64'), (Join-Path $stage '*.py'), (Join-Path $stage 'build-manifest.json'))
    Compress-Archive -Path $items -DestinationPath $archive
    Write-Output $archive
} finally {
    $env:GOOS=$oldOS; $env:GOARCH=$oldArch; $env:CGO_ENABLED=$oldCGO; $env:SEE_SIZE_AGENT_TOKEN=$oldToken
    Pop-Location
}
