package export

import (
	"context"
	"os/exec"
	"time"
)

// toolStopGrace is how long a cancelled VLC/FFmpeg child gets to exit after the
// stop signal before it is killed.
const toolStopGrace = 2 * time.Second

// toolCommand runs an external tool tied to the job's context: cancelling the job
// (Cancel, Stop, engine shutdown) signals the child to stop, then kills it after
// toolStopGrace, and Wait returns promptly even if the child left pipes open.
func toolCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Cancel = func() error { return interruptProcess(cmd) }
	cmd.WaitDelay = toolStopGrace
	return cmd
}
