param([Parameter(Mandatory)][int]$OwnerPid,[switch]$Restore)
$ErrorActionPreference = 'Stop'
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class QimengSmokeWindow {
    private delegate bool WindowCallback(IntPtr hwnd, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool EnumWindows(WindowCallback next, IntPtr arg);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint pid);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr GetWindow(IntPtr window,uint command);
    [DllImport("user32.dll")] public static extern bool IsZoomed(IntPtr window);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr window);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr window, int show);
    public static IntPtr Library(uint owner) {
        IntPtr found = IntPtr.Zero;
        EnumWindows(delegate(IntPtr window, IntPtr _) {
            uint pid;
            GetWindowThreadProcessId(window, out pid);
            if (pid == owner && IsWindowVisible(window) && GetWindow(window, 4) == IntPtr.Zero) {
                found = window;
                return false;
            }
            return true;
        }, IntPtr.Zero);
        return found;
    }
}
"@
$handle = [QimengSmokeWindow]::Library($OwnerPid)
if ($handle -eq [IntPtr]::Zero) { throw 'No visible top-level window belongs to this isolated app process' }
if ($Restore) { [void][QimengSmokeWindow]::ShowWindow($handle,9) }
@{maximized=[QimengSmokeWindow]::IsZoomed($handle); minimized=[QimengSmokeWindow]::IsIconic($handle)} | ConvertTo-Json -Compress
