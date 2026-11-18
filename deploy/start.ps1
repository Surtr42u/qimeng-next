param([switch]$ResetPassword, [switch]$NoBrowser)
$ErrorActionPreference = 'Stop'
$packageRoot = [IO.Path]::GetFullPath($PSScriptRoot)
$state = if ($env:QIMENG_PACKAGE_STATE) { $env:QIMENG_PACKAGE_STATE } else { Join-Path $env:LOCALAPPDATA 'QimengNextServer' }
if (-not [IO.Path]::IsPathRooted($state)) { throw '数据目录必须使用绝对路径。' }
$state = [IO.Path]::GetFullPath($state)
if ($state.TrimEnd('\') -eq $packageRoot.TrimEnd('\') -or $state.StartsWith($packageRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) { throw '请将持久数据目录放在解压位置之外。' }
$port = if ($env:QIMENG_PACKAGE_PORT) { [int]$env:QIMENG_PACKAGE_PORT } else { 8421 }
if ($port -lt 1 -or $port -gt 65535) { throw '端口无效。' }
$marker = Join-Path $state '.qimeng-next-package'
if (Test-Path -LiteralPath $marker) {
  if ([IO.File]::ReadAllText($marker).Trim() -ne 'qimeng-next-package-v1') { throw '无法确认数据目录归属。' }
} else {
  if ((Test-Path -LiteralPath $state) -and @(Get-ChildItem -Force -LiteralPath $state | Select-Object -First 1).Count) { throw '该位置已有其他文件，请选择空的专用数据目录。' }
  [void][IO.Directory]::CreateDirectory($state)
  [IO.File]::WriteAllText($marker, 'qimeng-next-package-v1')
}
$data = Join-Path $state 'data'
$media = Join-Path $state 'media'
$private = Join-Path $state 'private'
foreach ($path in @($data, (Join-Path $media '我的影库'), $private)) { [void][IO.Directory]::CreateDirectory($path) }
$secretFile = Join-Path $private 'login.dpapi'
$plain = $null
$secure = $null
if ($env:QIMENG_PACKAGE_PASSWORD_FILE) {
  $plain = [IO.File]::ReadAllText($env:QIMENG_PACKAGE_PASSWORD_FILE).TrimEnd([char]13,[char]10)
} else {
  if ((Test-Path -LiteralPath $secretFile) -and -not $ResetPassword) {
    try { $secure = ConvertTo-SecureString -String ([IO.File]::ReadAllText($secretFile)) }
    catch { throw '当前 Windows 账号不能解密已存口令，可用 start.cmd -ResetPassword 重设。' }
  } else {
    $secure = Read-Host '设置影库登录密码（至少 12 个字符，输入隐藏）' -AsSecureString
  }
  $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
  try { $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) }
  finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) }
}
if ([Text.Encoding]::UTF32.GetByteCount($plain) / 4 -lt 12 -or [Text.Encoding]::UTF8.GetByteCount($plain) -gt 1024) { throw '密码至少需要 12 个字符，且最多 1024 字节。' }
if ($secure -and ($ResetPassword -or -not (Test-Path -LiteralPath $secretFile))) {
  $temporary = Join-Path $private ([Guid]::NewGuid().ToString('N') + '.new')
  [IO.File]::WriteAllText($temporary, (ConvertFrom-SecureString $secure))
  if ([IO.File]::Exists($secretFile)) { [IO.File]::Replace($temporary, $secretFile, $null) } else { [IO.File]::Move($temporary, $secretFile) }
}
$launch = [Guid]::NewGuid().ToString('N')
$receipt = Join-Path $private ('launch-' + $launch + '.json')
$random = [byte[]]::new(32)
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
try { $rng.GetBytes($random) } finally { $rng.Dispose() }
$start = [Diagnostics.ProcessStartInfo]::new()
$start.FileName = Join-Path $packageRoot 'qimeng-next.exe'
$start.WorkingDirectory = $packageRoot
$start.UseShellExecute = $false
foreach ($name in @($start.EnvironmentVariables.Keys)) { if ($name -like 'QIMENG_*') { $start.EnvironmentVariables.Remove($name) } }
$start.EnvironmentVariables['QIMENG_ADMIN_TOKEN'] = [Convert]::ToBase64String($random)
$start.EnvironmentVariables['QIMENG_ADMIN_PASSWORD'] = $plain
$start.EnvironmentVariables['QIMENG_DATA_DIR'] = $data
$start.EnvironmentVariables['QIMENG_STORAGE_ROOTS'] = (ConvertTo-Json @{media=$media} -Compress)
$start.EnvironmentVariables['QIMENG_LISTEN'] = "127.0.0.1:$port"
$start.EnvironmentVariables['QIMENG_WEB_DIR'] = Join-Path $packageRoot 'web'
$start.EnvironmentVariables['QIMENG_STARTUP_RECEIPT'] = $receipt
$start.EnvironmentVariables['QIMENG_LAUNCH_ID'] = $launch
$plain = $null
if ($secure) { $secure.Dispose() }
$process = [Diagnostics.Process]::Start($start)
$start.EnvironmentVariables.Remove('QIMENG_ADMIN_TOKEN')
$start.EnvironmentVariables.Remove('QIMENG_ADMIN_PASSWORD')
try {
  $deadline = [DateTime]::UtcNow.AddSeconds(60)
  while (-not $process.HasExited -and -not (Test-Path -LiteralPath $receipt) -and [DateTime]::UtcNow -lt $deadline) { Start-Sleep -Milliseconds 150 }
  if ($process.HasExited) { throw '服务未启动。请确认该端口和数据目录没有被另一服务占用。' }
  if (-not (Test-Path -LiteralPath $receipt)) { Write-Host '服务尚未确认就绪，请查看运行状态。' }
  else {
    $ready = [IO.File]::ReadAllText($receipt) | ConvertFrom-Json
    if ($ready.launchId -ne $launch -or $ready.listenAddress -ne "127.0.0.1:$port") { throw '启动回执与本次实例不匹配。' }
    $origin = "http://127.0.0.1:$port/"
    Write-Host "影库已就绪。请在浏览器打开 $origin 。保持此窗口打开，按 Ctrl+C 停止。"
    if (-not $NoBrowser) { Start-Process -FilePath $origin }
  }
  while (-not $process.WaitForExit(400)) {}
  if ($process.ExitCode -ne 0) { throw '影库服务已异常退出。' }
} finally {
  if ([IO.File]::Exists($receipt)) { [IO.File]::Delete($receipt) }
  $process.Dispose()
}
