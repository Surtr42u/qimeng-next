package service

import (
 "os/exec"
 "time"
)

func configureMediaProcess(cmd *exec.Cmd){cmd.WaitDelay=5*time.Second;cmd.Env=mediaEnvironment()}
