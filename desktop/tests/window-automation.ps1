param([Parameter(Mandatory)][int]$OwnerPid, [string]$FirstOrigin, [string]$SecondOrigin,
    [string]$OfflineOrigin, [string]$ConfigDirectory, [string]$Evidence, [switch]$Restored)
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'This test requires a hosted Windows runner.'
}
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Drawing
Add-Type @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public static class QimengTestWindow {
    private delegate bool Visit(IntPtr window, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool EnumWindows(Visit next, IntPtr param);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint pid);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] public static extern bool IsWindow(IntPtr window);
    [DllImport("user32.dll")] public static extern bool IsZoomed(IntPtr window);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr window);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr window, int action);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr window);
    [StructLayout(LayoutKind.Sequential)] public struct Point { public int X, Y; }
    [DllImport("user32.dll")] private static extern IntPtr WindowFromPoint(Point point);
    [DllImport("user32.dll")] private static extern IntPtr GetAncestor(IntPtr window, uint flags);
    [DllImport("user32.dll")] private static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] private static extern void mouse_event(uint flags, uint x, uint y, uint data, UIntPtr extra);
    public static bool ClickInWindow(IntPtr owner, int x, int y) {
        var point = new Point { X = x, Y = y };
        if (GetAncestor(WindowFromPoint(point), 2) != owner || !SetCursorPos(x, y)) return false;
        mouse_event(2, 0, 0, 0, UIntPtr.Zero);
        mouse_event(4, 0, 0, 0, UIntPtr.Zero);
        return true;
    }
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr window, IntPtr device, uint flags);
    public static IntPtr[] Visible(uint owner) {
        var windows = new List<IntPtr>();
        EnumWindows(delegate(IntPtr w, IntPtr p) {
            uint pid;
            GetWindowThreadProcessId(w, out pid);
            if (pid == owner && IsWindowVisible(w)) windows.Add(w);
            return true;
        }, IntPtr.Zero);
        return windows.ToArray();
    }
}
"@

function Wait-Until([string]$description, [scriptblock]$operation) {
    $until = [datetime]::UtcNow.AddSeconds(35)
    do {
        try {
            $value = & $operation
            if ($value) { return $value }
        } catch [System.Windows.Automation.ElementNotAvailableException] {
            # Navigation and window replacement invalidate the old accessibility node.
        }
        Start-Sleep -Milliseconds 180
    } while ([datetime]::UtcNow -lt $until)
    $count = [QimengTestWindow]::Visible($OwnerPid).Length
    throw "Timed out waiting for $description ($count visible application windows)."
}

function Get-Control([string]$name, [System.Windows.Automation.ControlType]$type, [IntPtr]$within = [IntPtr]::Zero, [bool]$enabled = $true) {
    Wait-Until $name {
        $handles = if ($within -eq [IntPtr]::Zero) { [QimengTestWindow]::Visible($OwnerPid) } else { @($within) }
        foreach ($hwnd in $handles) {
            $root = [System.Windows.Automation.AutomationElement]::FromHandle($hwnd)
            $byName = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::NameProperty, $name)
            $match = $byName
            if ($type) {
                $byType = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ControlTypeProperty, $type)
                $match = [System.Windows.Automation.AndCondition]::new($byName, $byType)
            }
            $element = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $match)
            if ($element -and $element.Current.IsEnabled -eq $enabled) { return @{ Handle = $hwnd; Element = $element } }
        }
    }
}

function Invoke-Control($control) {
    $control.Element.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
}

function Save-DialogImage($control) {
    $window = [System.Windows.Automation.AutomationElement]::FromHandle($control.Handle)
    $rectangle = $window.Current.BoundingRectangle
    $bitmap = [Drawing.Bitmap]::new([int]$rectangle.Width, [int]$rectangle.Height)
    $graphics = [Drawing.Graphics]::FromImage($bitmap)
    try {
        $dc = $graphics.GetHdc()
        try { $captured = [QimengTestWindow]::PrintWindow($control.Handle, $dc, 2) }
        finally { $graphics.ReleaseHdc($dc) }
        if ($captured) { $bitmap.Save((Join-Path $Evidence 'native-connection.png'), [Drawing.Imaging.ImageFormat]::Png) }
    } finally { $graphics.Dispose(); $bitmap.Dispose() }
}

function Connect-Library($inputControl, [string]$origin) {
    $inputControl.Element.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern).SetValue($origin)
    Invoke-Control (Get-Control '保存并连接' ([System.Windows.Automation.ControlType]::Button) $inputControl.Handle)
}

function Get-SavedOrigin([string]$name) {
    (Get-Content -LiteralPath (Join-Path $ConfigDirectory $name) -Raw -Encoding UTF8 | ConvertFrom-Json).origin
}

function Confirm-Rejection([string]$message, [string]$attemptedOrigin) {
    [void](Get-Control $message $null $login.Handle)
    $visible = [QimengTestWindow]::Visible($OwnerPid)
    if ($visible -notcontains $login.Handle -or $visible -notcontains $library -or $visible -notcontains $media) {
        throw 'The failed switch did not preserve the settings, library and original windows.'
    }
    $field = Get-Control '服务器地址' $edit $login.Handle
    if ($field.Element.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern).Current.Value -cne $attemptedOrigin) {
        throw 'The failed address must remain editable for a retry.'
    }
    [void](Get-Control '保存并连接' $button $login.Handle)
    if ((Get-SavedOrigin 'connection.json') -cne "$FirstOrigin/" -or
        (Test-Path -LiteralPath (Join-Path $ConfigDirectory 'connection.previous.json'))) {
        throw 'The failed switch changed the saved connection.'
    }
}

function Close-Library($control) {
    try { Invoke-Control $control } catch [System.Windows.Automation.ElementNotAvailableException] {}
}

$button = [System.Windows.Automation.ControlType]::Button
if ($Restored) {
    Close-Library (Get-Control 'Close two' $button)
    Write-Output 'Verified the second library reopened on a normal launch.'
    return
}
$edit = [System.Windows.Automation.ControlType]::Edit
$login = Get-Control '服务器地址' $edit
Save-DialogImage $login
Connect-Library $login $FirstOrigin
Write-Output 'Opened the real connection form.'
$maximize = Get-Control 'Maximize one' $button
$library = $maximize.Handle
$wasMaximized = [QimengTestWindow]::IsZoomed($library)
Invoke-Control $maximize
[void](Wait-Until 'native maximize' { [QimengTestWindow]::IsZoomed($library) -ne $wasMaximized })
Invoke-Control (Get-Control 'Minimize one' $button $library)
[void](Wait-Until 'native minimize' { [QimengTestWindow]::IsIconic($library) })
[void][QimengTestWindow]::ShowWindow($library, 9)
[void][QimengTestWindow]::SetForegroundWindow($library)
$link = Get-Control 'Open original' ([System.Windows.Automation.ControlType]::Hyperlink) $library
[void](Wait-Until 'clickable original link in the active library window' {
    $rect = $link.Element.Current.BoundingRectangle
    if (!$rect.IsEmpty) {
        [QimengTestWindow]::ClickInWindow($library, [int]($rect.X + $rect.Width / 2), [int]($rect.Y + $rect.Height / 2))
    }
})
$media = (Get-Control 'Original media proof' $button).Handle
if ($media -eq $library) { throw 'The original did not open in a separate window.' }
Write-Output 'Verified native titlebar and original media window.'
# Bring the existing settings window back and exercise its production save path again.
[void][QimengTestWindow]::ShowWindow($login.Handle, 9)
[void][QimengTestWindow]::SetForegroundWindow($login.Handle)
Connect-Library (Get-Control '服务器地址' $edit $login.Handle) $OfflineOrigin
Confirm-Rejection '无法连接影库，请检查服务器地址和网络后重试' $OfflineOrigin
Connect-Library (Get-Control '服务器地址' $edit $login.Handle) $SecondOrigin
Confirm-Rejection '此地址未返回有效的影库实例信息，请检查地址后重试' $SecondOrigin
Invoke-Control (Get-Control '保存并连接' $button $login.Handle)
Confirm-Rejection '实例地址发生重定向，请填写最终服务器地址后重试' $SecondOrigin
Invoke-Control (Get-Control '保存并连接' $button $login.Handle)
[void](Get-Control '正在连接…' $button $login.Handle $false)
Confirm-Rejection '影库响应超时，请稍后重试' $SecondOrigin
$beforeRetry = [QimengTestWindow]::IsZoomed($library)
Invoke-Control (Get-Control 'Maximize one' $button $library)
[void](Wait-Until 'first library still owns its native titlebar' { [QimengTestWindow]::IsZoomed($library) -ne $beforeRetry })
[void](Get-Control 'Original media proof' $button $media)
Write-Output 'Verified failed switches preserve the active library, original, saved address and retry form.'
[void][QimengTestWindow]::SetForegroundWindow($login.Handle)
Invoke-Control (Get-Control '保存并连接' $button $login.Handle)
$close = Get-Control 'Close two' $button
[void](Wait-Until 'previous windows to close' { -not [QimengTestWindow]::IsWindow($library) -and -not [QimengTestWindow]::IsWindow($media) })
if ((Get-SavedOrigin 'connection.json') -cne "$SecondOrigin/" -or (Get-SavedOrigin 'connection.previous.json') -cne "$FirstOrigin/") {
    throw 'A successful switch must save the new origin and preserve the previous one.'
}
Close-Library $close
Write-Output 'Verified server switching and requested native close.'
