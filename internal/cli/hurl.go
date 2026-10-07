package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"hookcheck"
)

type hurlCheck struct {
	binary string
	file   string
}

func prepareHurl(name, scenarioPath string, allowed bool) (*hurlCheck, error) {
	if name == "" {
		return nil, nil
	}
	if !allowed {
		return nil, fmt.Errorf("scenario has a Hurl file; pass --allow-hurl to execute it")
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(filepath.Dir(scenarioPath), name)
	}
	path, err := filepath.Abs(name)
	if err != nil {
		return nil, fmt.Errorf("invalid Hurl file path")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Hurl file must exist and be a regular file")
	}
	binary, err := exec.LookPath("hurl")
	if err != nil {
		return nil, fmt.Errorf("Hurl executable not found; install Hurl or remove hurl_file")
	}
	return &hurlCheck{binary: binary, file: path}, nil
}

func (h *hurlCheck) run(parent context.Context, baseURL string) hookcheck.CheckResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.binary, "--test", "--variable", "base_url="+baseURL, h.file)
	// Nil streams go directly to the null device: no child-held copy pipes.
	// External test output may contain credentials or response bodies.
	cmd.WaitDelay = 250 * time.Millisecond
	configureProcess(cmd)
	err := cmd.Run()
	result := hookcheck.CheckResult{Name: "Hurl", Polls: 1, DurationMS: time.Since(start).Milliseconds(), Outcome: "passed"}
	if err == nil {
		return result
	}
	switch {
	case parent.Err() != nil:
		result.Outcome = "cancelled"
		result.Message = "Hurl cancelled"
	case ctx.Err() != nil:
		result.Outcome = "timeout"
		result.Message = "Hurl exceeded its 30s execution limit"
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 4) {
			result.Outcome = "failed"
			result.Message = "Hurl HTTP request or assertion failed; run the file directly for details"
		} else {
			result.Outcome = "external_error"
			result.Message = "Hurl configuration or process execution failed"
		}
	}
	return result
}
