param(
    [switch]$AsDesktopUser,
    [string]$NodeExecutable,
    [string]$SourceCommit,
    [string]$WorkerLog
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'Run this check on an isolated GitHub-hosted Windows runner.'
}
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$isElevated = ([Security.Principal.WindowsPrincipal]::new($identity)).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)

if ($AsDesktopUser) {
    $workerResult = 1
    try {
        if ($isElevated) { throw 'The desktop test worker has administrator rights.' }
        if ($SourceCommit -notmatch '^[0-9a-f]{40}$') { throw 'Missing tested source commit.' }
        Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
        $env:GITHUB_SHA = $SourceCommit
        $env:PATH = "$PSHOME;$env:PATH"
        Set-Content -LiteralPath $WorkerLog -Value 'Verified ordinary-user token. Opening the built installer.'
        & $NodeExecutable tests/cloud-webview.mjs *>> $WorkerLog
        $workerResult = $LASTEXITCODE
    } catch {
        Add-Content -LiteralPath $WorkerLog -Value $_.Exception.Message
    } finally {
        [IO.File]::WriteAllText("$WorkerLog.exit.tmp", [string]$workerResult)
        [IO.File]::Move("$WorkerLog.exit.tmp", "$WorkerLog.exit")
    }
    exit $workerResult
}

if (-not $isElevated) {
    & node tests/cloud-webview.mjs
    exit $LASTEXITCODE
}

# Use the existing desktop and its user profile with Windows' Basic User token.
# The child checks its effective rights before opening the installed application.
$nonce = [guid]::NewGuid().ToString('N')
$taskName = 'QimengWindow-' + $nonce
$stageParent = (Get-Item -LiteralPath $env:ProgramData -Force).FullName
$stage = Join-Path $stageParent $taskName
$desktop = Join-Path $stage 'desktop'
$tests = Join-Path $desktop 'tests'
$nsis = Join-Path $desktop 'src-tauri/target/release/bundle/nsis'
$evidence = Join-Path $stage 'build/desktop-evidence'
$artifact = [IO.Path]::GetFullPath((Join-Path $PWD '../build/desktop-evidence'))
$logPath = Join-Path $evidence 'worker.log'
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
    $sid = $identity.User.Value
    & icacls.exe $stage /grant "*${sid}:(OI)(CI)RX" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant access to the staged installer.' }
    & icacls.exe $evidence /grant "*${sid}:(OI)(CI)M" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant access to the evidence directory.' }
    $worker = Join-Path $tests 'run-desktop-smoke.ps1'
    $shellPath = (Get-Process -Id $PID).Path
    $launch = '"{0}" -NoProfile -NonInteractive -WindowStyle Hidden -File "{1}" -AsDesktopUser -NodeExecutable "{2}" -SourceCommit "{3}" -WorkerLog "{4}"' -f $shellPath, $worker, $nodePath, $env:GITHUB_SHA, $logPath
    & runas.exe /env /trustlevel:0x20000 $launch
    if ($LASTEXITCODE -ne 0) { throw 'Windows could not start the desktop worker at the Basic User trust level.' }
    $deadline = [datetime]::UtcNow.AddMinutes(7)
    $startupDeadline = [datetime]::UtcNow.AddSeconds(35)
    $receipt = "$logPath.exit"
    while (-not (Test-Path -LiteralPath $receipt) -and [datetime]::UtcNow -lt $deadline) {
        if ([datetime]::UtcNow -ge $startupDeadline -and -not (Test-Path -LiteralPath $logPath)) {
            throw 'Windows did not start the Basic User worker in the current desktop.'
        }
        Start-Sleep -Seconds 1
    }
    if (Test-Path -LiteralPath $logPath) { Get-Content -LiteralPath $logPath -Tail 35 }
    if (-not (Test-Path -LiteralPath $receipt)) {
        throw 'The Basic User worker did not finish its installed-window checks.'
    }
    if ([IO.File]::ReadAllText($receipt) -ne '0') {
        throw 'The standard-user smoke check reported an error.'
    }
} finally {
    foreach ($name in @('native-connection.png', 'native-window.json')) {
        $resultFile = Join-Path $evidence $name
        if (Test-Path -LiteralPath $resultFile -PathType Leaf) { Copy-Item -LiteralPath $resultFile -Destination $artifact }
    }
    if (Test-Path -LiteralPath $stage) {
        $directory = Get-Item -LiteralPath $stage -Force
        if ($directory.Parent.FullName -ne $stageParent -or $directory.Name -ne $taskName -or
            ($directory.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Unexpected smoke staging path.' }
        Remove-Item -LiteralPath $directory.FullName -Recurse -Force
    }
}
