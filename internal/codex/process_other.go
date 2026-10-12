//go:build !unix

package codex

import "os/exec"

// Keep the application buildable on other platforms; New disables this
// optional integration until equivalent process-tree cleanup is provided.
const processGroupsSupported = false

func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
