param([Parameter(Mandatory)][int]$OwnerPid, [string]$FirstOrigin, [string]$SecondOrigin, [string]$Evidence)
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

function Get-Control([string]$name, [System.Windows.Automation.ControlType]$type, [IntPtr]$within = [IntPtr]::Zero) {
    Wait-Until $name {
        $handles = if ($within -eq [IntPtr]::Zero) { [QimengTestWindow]::Visible($OwnerPid) } else { @($within) }
        foreach ($hwnd in $handles) {
            $root = [System.Windows.Automation.AutomationElement]::FromHandle($hwnd)
            $byName = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::NameProperty, $name)
            $byType = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ControlTypeProperty, $type)
            $match = [System.Windows.Automation.AndCondition]::new($byName, $byType)
            $element = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $match)
            if ($element -and $element.Current.IsEnabled) { return @{ Handle = $hwnd; Element = $element } }
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

$button = [System.Windows.Automation.ControlType]::Button
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
Invoke-Control (Get-Control 'Open original' ([System.Windows.Automation.ControlType]::Hyperlink) $library)
$media = (Get-Control 'Original media proof' $button).Handle
if ($media -eq $library) { throw 'The original did not open in a separate window.' }
Write-Output 'Verified native titlebar and original media window.'
# Bring the existing settings window back and exercise its production save path again.
[void][QimengTestWindow]::ShowWindow($login.Handle, 9)
[void][QimengTestWindow]::SetForegroundWindow($login.Handle)
Connect-Library (Get-Control '服务器地址' $edit $login.Handle) $SecondOrigin
$close = Get-Control 'Close two' $button
[void](Wait-Until 'previous windows to close' { -not [QimengTestWindow]::IsWindow($library) -and -not [QimengTestWindow]::IsWindow($media) })
try { Invoke-Control $close } catch [System.Windows.Automation.ElementNotAvailableException] {}
Write-Output 'Verified server switching and requested native close.'
