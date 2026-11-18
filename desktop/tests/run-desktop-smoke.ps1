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
        if ($isElevated) { throw 'The desktop test worker has administrator rights.' }
        if ($env:USERNAME -notmatch '^qmtest[0-9a-f]{12}$') { throw 'Expected the temporary test account.' }
        if ($SourceCommit -notmatch '^[0-9a-f]{40}$') { throw 'Missing tested source commit.' }
        $env:GITHUB_ACTIONS = 'true'
        $env:GITHUB_SHA = $SourceCommit
        $env:PATH = "$PSHOME;$env:PATH"
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

if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'Run this check on an isolated GitHub-hosted Windows runner.'
}
if (-not $isElevated) {
    & node tests/cloud-webview.mjs
    exit $LASTEXITCODE
}

# A limited task for the runner's own administrator account can still be elevated.
# Give a separate standard account read access only to the installer and its test driver.
$nonce = [guid]::NewGuid().ToString('N')
$taskName = 'QimengWindow-' + $nonce
$accountName = 'qmtest' + $nonce.Substring(0, 12)
$stageParent = (Get-Item -LiteralPath $env:ProgramData).FullName
$stage = Join-Path $stageParent $taskName
$desktop = Join-Path $stage 'desktop'
$tests = Join-Path $desktop 'tests'
$nsis = Join-Path $desktop 'src-tauri/target/release/bundle/nsis'
$evidence = Join-Path $stage 'build/desktop-evidence'
$artifact = [IO.Path]::GetFullPath((Join-Path $PWD '../build/desktop-evidence'))
$logPath = Join-Path $evidence 'worker.log'
$account = $null
$task = $null
try {
    foreach ($dir in @($tests, $nsis, $evidence, $artifact)) {
        [void][IO.Directory]::CreateDirectory($dir)
    }
    $installers = @(Get-ChildItem -LiteralPath 'src-tauri/target/release/bundle/nsis' -File -Filter '*-setup.exe')
    if ($installers.Count -ne 1) { throw 'Expected one installer from this build.' }
    Copy-Item -LiteralPath $installers[0].FullName -Destination $nsis
    foreach ($driver in @('run-desktop-smoke.ps1', 'cloud-webview.mjs', 'window-state.ps1')) {
        Copy-Item -LiteralPath (Join-Path 'tests' $driver) -Destination $tests
    }
    $nodePath = Join-Path $stage 'node.exe'
    Copy-Item -LiteralPath (Get-Command node.exe -CommandType Application).Source -Destination $nodePath
    $password = ConvertTo-SecureString ('aA9!' + [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))) -AsPlainText -Force
    $account = New-LocalUser -Name $accountName -Password $password
    $users = Get-LocalGroup -SID 'S-1-5-32-545'
    if (-not (Get-LocalGroupMember -Group $users | Where-Object SID -eq $account.SID)) {
        Add-LocalGroupMember -Group $users -Member $account
    }
    $sid = $account.SID.Value
    & icacls.exe $stage /grant "*${sid}:(OI)(CI)RX" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant access to the staged installer.' }
    & icacls.exe $evidence /grant "*${sid}:(OI)(CI)M" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant access to the evidence directory.' }
    $worker = Join-Path $tests 'run-desktop-smoke.ps1'
    $arguments = '-NoProfile -NonInteractive -WindowStyle Hidden -File "{0}" -AsDesktopUser -NodeExecutable "{1}" -SourceCommit "{2}" -WorkerLog "{3}"' -f $worker, $nodePath, $env:GITHUB_SHA, $logPath
    $action = New-ScheduledTaskAction -Execute (Get-Process -Id $PID).Path -Argument $arguments -WorkingDirectory $desktop
    $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 8)
    $credential = [pscredential]::new("$env:COMPUTERNAME\$accountName", $password)
    $task = Register-ScheduledTask -TaskName $taskName -Action $action -Settings $settings -User $credential.UserName -Password $credential.GetNetworkCredential().Password -RunLevel Limited
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
    if ($task) {
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    }
    foreach ($name in @('native-connection.png', 'native-window.json')) {
        $resultFile = Join-Path $evidence $name
        if (Test-Path -LiteralPath $resultFile -PathType Leaf) { Copy-Item -LiteralPath $resultFile -Destination $artifact }
    }
    if ($account) { Remove-LocalUser -SID $account.SID }
    if (Test-Path -LiteralPath $stage) {
        $directory = Get-Item -LiteralPath $stage
        if ($directory.Parent.FullName -ne $stageParent -or $directory.Name -ne $taskName -or
            ($directory.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Unexpected smoke staging path.' }
        Remove-Item -LiteralPath $directory.FullName -Recurse -Force
    }
}
