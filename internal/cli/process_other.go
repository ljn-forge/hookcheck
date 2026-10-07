//go:build !unix

package cli

import "os/exec"

// Other platforms retain CommandContext's immediate-process cancellation.
func configureProcess(cmd *exec.Cmd) {}
