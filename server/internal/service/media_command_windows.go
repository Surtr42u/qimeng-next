package service

import (
 "os/exec"
 "syscall"
 "time"
)

func configureMediaProcess(cmd *exec.Cmd){cmd.WaitDelay=5*time.Second;cmd.SysProcAttr=&syscall.SysProcAttr{CreationFlags:0x08000000,HideWindow:true}}
