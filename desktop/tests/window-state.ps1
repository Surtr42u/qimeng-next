param([Parameter(Mandatory)][int]$OwnerPid,[switch]$Restore)
$ErrorActionPreference = 'Stop'
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class QimengSmokeWindow {
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr window, out uint pid);
    [DllImport("user32.dll")] public static extern bool IsZoomed(IntPtr window);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr window);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr window, int show);
}
"@
$handle = [QimengSmokeWindow]::FindWindow($null, '绮梦影库')
$processOwner = [uint32]0
if ($handle -eq [IntPtr]::Zero) { throw 'Synthetic main window not found' }
[void][QimengSmokeWindow]::GetWindowThreadProcessId($handle,[ref]$processOwner)
if ($processOwner -ne $OwnerPid) { throw 'The selected window is not owned by this CI process' }
if ($Restore) { [void][QimengSmokeWindow]::ShowWindow($handle,9) }
@{maximized=[QimengSmokeWindow]::IsZoomed($handle); minimized=[QimengSmokeWindow]::IsIconic($handle)} | ConvertTo-Json -Compress
