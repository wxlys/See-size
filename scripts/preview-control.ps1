#Requires -Version 7.0
param(
    [ValidateSet('Start','Stop','Status','Diagnose','Run')][string]$Action='Status',
    [ValidatePattern('^[a-zA-Z0-9][a-zA-Z0-9_.-]*$')][string]$SshHost='wsrser',
    [ValidateRange(1024,65535)][int]$LocalPort=18082,
    [ValidateRange(1024,65535)][int]$RemotePort=18081
)
$ErrorActionPreference='Stop'
$stateDir=Join-Path $env:LOCALAPPDATA 'SeeSize/preview'
[void][IO.Directory]::CreateDirectory($stateDir)
$stateFile=Join-Path $stateDir "$LocalPort.json"
$logFile=Join-Path $stateDir "$LocalPort.log"
$scriptFile=$PSCommandPath
$forward="127.0.0.1:${LocalPort}:127.0.0.1:${RemotePort}"
function Write-Event([string]$message) {
    $lines=@()
    if(Test-Path -LiteralPath $logFile){$lines=@(Get-Content -LiteralPath $logFile -Tail 199)}
    @($lines;"$([DateTime]::Now.ToString('s')) $message") | Set-Content -LiteralPath $logFile -Encoding utf8
}
function Read-State {
    if(Test-Path -LiteralPath $stateFile){try{return Get-Content -LiteralPath $stateFile -Raw | ConvertFrom-Json}catch{return $null}}
    return $null
}
function Match-Process($record,[bool]$worker) {
    if(!$record){return $null}
    $p=Get-Process -Id $record.id -ErrorAction SilentlyContinue
    if(!$p -or $p.StartTime.ToUniversalTime().Ticks.ToString() -ne $record.started){return $null}
    $info=Get-CimInstance Win32_Process -Filter "ProcessId=$($p.Id)"
    if($worker){if(!$info.CommandLine.Contains($scriptFile) -or $info.CommandLine -notmatch '-Action\s+Run'){return $null}}
    else {if($info.Name -ne 'ssh.exe' -or !$info.CommandLine.Contains($record.forward)){return $null}}
    return $p
}
function Identity($p) {return @{id=$p.Id;started=$p.StartTime.ToUniversalTime().Ticks.ToString()}}
function Save-State($state) {
    $temp="$stateFile.tmp"
    $state | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $temp -Encoding utf8
    [IO.File]::Move($temp,$stateFile,$true)
}
function Listener {return Get-NetTCPConnection -LocalPort $LocalPort -State Listen -ErrorAction SilentlyContinue}
if($Action -eq 'Run') {
    # Exclusive lock prevents duplicate supervisors, including concurrent Start calls.
    try{$lock=[IO.File]::Open((Join-Path $stateDir "$LocalPort.lock"),[IO.FileMode]::OpenOrCreate,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None)}catch{exit 2}
    $child=$null
    try{
        if(Listener){Write-Event 'Port occupied; refusing to replace existing listener.';exit 3}
        $state=@{host=$SshHost;local_port=$LocalPort;remote_port=$RemotePort;worker=(Identity (Get-Process -Id $PID));child=$null}
        Save-State $state
        Write-Event "Supervisor started: $SshHost -> $forward"
        while($true){
            if(Listener){Write-Event 'Port occupied before retry; supervisor stopped.';break}
            $child=Start-Process -FilePath (Get-Command ssh.exe).Source -ArgumentList @('-N','-L',$forward,'-o','BatchMode=yes','-o','ExitOnForwardFailure=yes','-o','ConnectTimeout=10','-o','ServerAliveInterval=30','-o','ServerAliveCountMax=3',$SshHost) -WindowStyle Hidden -PassThru
            $state.child=Identity $child;$state.child.forward=$forward;Save-State $state
            Write-Event "SSH started PID=$($child.Id)"
            $child.WaitForExit();Write-Event "SSH exited code=$($child.ExitCode); retry in 5s"
            $state.child=$null;Save-State $state
            Start-Sleep -Seconds 5
        }
    }finally{
        if($child -and !$child.HasExited){$child.Kill()}
        $lock.Dispose()
    }
    exit
}
$state=Read-State
$worker=if($state){Match-Process $state.worker $true}else{$null}
if($Action -eq 'Stop') {
    if(!$worker){Write-Output 'No matching managed supervisor; unknown processes were not stopped.';exit}
    # Stop retry loop first, then its validated SSH child. Re-read after stopping
    # to catch a child created just before the supervisor exited.
    Stop-Process -Id $worker.Id
    $worker.WaitForExit()
    $state=Read-State
    $child=Match-Process $state.child $false
    if($child){Stop-Process -Id $child.Id}
    Write-Event 'Stopped by preview-control.'
    Write-Output 'Managed preview stopped; remote services are unchanged.'
    exit
}
if($Action -eq 'Start') {
    if($worker){
        if($state.host -ne $SshHost -or $state.remote_port -ne $RemotePort){throw 'This port is managed with different settings. Stop it explicitly before changing the target.'}
        Write-Output "Already managed: http://127.0.0.1:$LocalPort/";exit
    }
    if(Listener){throw 'Local port is occupied by an unmanaged process. Use Diagnose; nothing was stopped.'}
    $pwsh=(Get-Process -Id $PID).Path
    Start-Process -FilePath $pwsh -ArgumentList @('-NoProfile','-File',('"'+$scriptFile+'"'),'-Action','Run','-SshHost',$SshHost,'-LocalPort',$LocalPort,'-RemotePort',$RemotePort) -WindowStyle Hidden | Out-Null
    for($i=0;$i -lt 10;$i++){
        Start-Sleep -Milliseconds 500
        try{$r=Invoke-WebRequest "http://127.0.0.1:$LocalPort/healthz" -TimeoutSec 1;if($r.StatusCode -eq 200){Write-Output "Ready: http://127.0.0.1:$LocalPort/";exit}}catch{}
    }
    Write-Output 'Started, but not healthy yet. Run Diagnose; see log for retry state.'
    exit
}
$listen=Listener
$health='unreachable'
try{$r=Invoke-WebRequest "http://127.0.0.1:$LocalPort/healthz" -TimeoutSec 3;$health="HTTP $($r.StatusCode)"}catch{}
[pscustomobject]@{Managed=[bool]$worker;Listening=[bool]$listen;Health=$health;SavedHost=$state.host;SavedRemotePort=$state.remote_port;Log=$logFile}
if($Action -eq 'Diagnose') {
    if($listen){$listen | Select-Object LocalAddress,LocalPort,OwningProcess}
    if(Test-Path -LiteralPath $logFile){Get-Content -LiteralPath $logFile -Tail 20}
    Write-Output 'Read-only remote connectivity check:'
    & ssh -o BatchMode=yes -o ConnectTimeout=8 $SshHost "curl --max-time 5 -s -o /dev/null -w '%{http_code}' http://127.0.0.1:$RemotePort/healthz"
}
