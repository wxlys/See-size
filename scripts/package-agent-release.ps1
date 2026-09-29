#Requires -Version 7.0
param(
    [Parameter(Mandatory)][string]$Go,
    [Parameter(Mandatory)][ValidatePattern('^v\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$')][string]$Version,
    [Parameter(Mandatory)][string]$HubRequirement,
    [Parameter(Mandatory)][string]$OutputDirectory
)
$ErrorActionPreference='Stop'
if(Test-Path -LiteralPath $OutputDirectory){throw 'Output directory exists; refusing overwrite'}
$repo=Split-Path $PSScriptRoot -Parent
Push-Location $repo
$oldOS=$env:GOOS; $oldArch=$env:GOARCH; $oldCGO=$env:CGO_ENABLED
try {
    $dirty=& git status --porcelain
    if($dirty){throw 'Commit intended changes first; release build requires a clean tree'}
    $commit=& git rev-parse HEAD
    $output=New-Item -ItemType Directory -Path $OutputDirectory
    $env:GOOS='linux';$env:GOARCH='amd64';$env:CGO_ENABLED='0'
    $hashes=@{}
    foreach($component in @('agent','enroll')) {
        $name="seesize-$component-linux-amd64"
        $path=Join-Path $output.FullName $name
        & $Go build -trimpath -o $path "./cmd/$component"
        if($LASTEXITCODE -ne 0){throw 'Build failed; partial output is NOT a release'}
        $hashes[$name]=(Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    Copy-Item (Join-Path $PSScriptRoot 'agent-setup.py') $output.FullName
    @{schema=1;version=$Version;platform='linux-amd64';source_commit=$commit;hub_requirement=$HubRequirement;sha256=$hashes} |
        ConvertTo-Json -Depth 4 | Set-Content (Join-Path $output.FullName 'agent-release.json') -Encoding utf8NoBOM
    Write-Output "Built local release assets: $($output.FullName). Nothing published. Verify in Linux before GitHub release."
} finally {
    $env:GOOS=$oldOS;$env:GOARCH=$oldArch;$env:CGO_ENABLED=$oldCGO
    Pop-Location
}
