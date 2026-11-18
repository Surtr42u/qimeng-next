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
    $workerResult = 1
    try {
        if ($isElevated -or $SourceCommit -notmatch '^[0-9a-f]{40}$') {
            throw 'An ordinary user session and a commit are required.'
        }
        $env:GITHUB_ACTIONS = 'true'
        $env:GITHUB_SHA = $SourceCommit
        & $NodeExecutable tests/cloud-webview.mjs *> $WorkerLog
        $workerResult = $LASTEXITCODE
    } catch {
        Add-Content -LiteralPath $WorkerLog -Value $_.Exception.Message
    } finally {
        [IO.File]::WriteAllText("$WorkerLog.exit.tmp", [string]$workerResult)
        [IO.File]::Move("$WorkerLog.exit.tmp", "$WorkerLog.exit")
    }
    exit $workerResult
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
    $receipt = "$logPath.exit"
    while (-not (Test-Path -LiteralPath $receipt) -and [datetime]::UtcNow -lt $deadline) {
        Start-Sleep -Seconds 1
    }
    if (Test-Path -LiteralPath $logPath) { Get-Content -LiteralPath $logPath -Tail 35 }
    if (-not (Test-Path -LiteralPath $receipt)) {
        $info = Get-ScheduledTaskInfo -TaskName $taskName
        throw "Standard-user desktop smoke timed out (scheduler result $($info.LastTaskResult))."
    }
    if ([IO.File]::ReadAllText($receipt) -ne '0') {
        throw 'The standard-user smoke check reported an error.'
    }
} finally {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
}
