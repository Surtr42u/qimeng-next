param(
    [switch]$AsDesktopUser,
    [string]$NodeExecutable,
    [string]$SourceCommit,
    [string]$WorkerLog
)
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$isElevated = ([Security.Principal.WindowsPrincipal]::new($identity)).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)

if ($AsDesktopUser) {
    if ($isElevated -or $SourceCommit -notmatch '^[0-9a-f]{40}$') {
        throw 'The isolated desktop window check requires an ordinary user session and a commit.'
    }
    $env:GITHUB_ACTIONS = 'true'
    $env:GITHUB_SHA = $SourceCommit
    & $NodeExecutable tests/cloud-webview.mjs *> $WorkerLog
    exit $LASTEXITCODE
}

if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'Run this check on an isolated GitHub-hosted Windows runner.'
}
if (-not $isElevated) {
    & node tests/cloud-webview.mjs
    exit $LASTEXITCODE
}

# The installed app normally runs with the desktop user's permissions.
# WebView2 150+ discards user-supplied debugging arguments for elevated hosts.
$taskName = 'QimengWindow-' + [guid]::NewGuid().ToString('N')
$logPath = Join-Path $env:RUNNER_TEMP ($taskName + '.log')
$nodePath = (Get-Command node.exe -CommandType Application).Source
$shellPath = (Get-Process -Id $PID).Path
$arguments = '-NoProfile -NonInteractive -WindowStyle Hidden -File "{0}" -AsDesktopUser -NodeExecutable "{1}" -SourceCommit "{2}" -WorkerLog "{3}"' -f $PSCommandPath, $nodePath, $env:GITHUB_SHA, $logPath
$action = New-ScheduledTaskAction -Execute $shellPath -Argument $arguments -WorkingDirectory (Get-Location).Path
$principal = New-ScheduledTaskPrincipal -UserId $identity.Name -RunLevel Limited -LogonType Interactive
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 8)
Register-ScheduledTask -TaskName $taskName -Action $action -Principal $principal -Settings $settings | Out-Null
try {
    Start-ScheduledTask -TaskName $taskName
    $deadline = [datetime]::UtcNow.AddMinutes(9)
    do {
        Start-Sleep -Seconds 1
        $info = Get-ScheduledTaskInfo -TaskName $taskName
        $state = (Get-ScheduledTask -TaskName $taskName).State
    } until (($info.LastRunTime.Year -gt 2000 -and $state -eq 'Ready') -or [datetime]::UtcNow -ge $deadline)
    if (Test-Path -LiteralPath $logPath) { Get-Content -LiteralPath $logPath -Tail 35 }
    if ($state -ne 'Ready' -or $info.LastRunTime.Year -le 2000 -or $info.LastTaskResult -ne 0) {
        throw "Standard-user desktop smoke failed (task state $state, result $($info.LastTaskResult))."
    }
} finally {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
}
