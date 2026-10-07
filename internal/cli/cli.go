package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ljn-forge/hookcheck"
)

// Run returns a process exit code without terminating the calling process.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "help", "--help", "-h":
		if _, err := fmt.Fprintln(stdout, usage); err != nil {
			return 2
		}
		return 0
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "version takes no arguments")
			return 2
		}
		if _, err := fmt.Fprintln(stdout, "hookcheck 0.1.0-dev"); err != nil {
			return 2
		}
		return 0
	case "run":
		return run(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

const usage = "Usage: hookcheck run --scenario FILE --base-url URL [--report FILE] [--allow-hurl]\n       hookcheck version"

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scenarioPath := flags.String("scenario", "", "versioned JSON scenario file")
	baseURL := flags.String("base-url", "", "HTTP(S) target origin")
	reportPath := flags.String("report", "", "write a private JSON report atomically")
	allowHurl := flags.Bool("allow-hurl", false, "allow the scenario to execute an external Hurl file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *scenarioPath == "" || *baseURL == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	file, err := os.Open(*scenarioPath)
	if err != nil {
		fmt.Fprintln(stderr, "could not open scenario file")
		return 2
	}
	s, loadErr := hookcheck.Load(file)
	closeErr := file.Close()
	if loadErr != nil {
		fmt.Fprintf(stderr, "invalid scenario: %v\n", loadErr)
		return 2
	}
	if closeErr != nil {
		fmt.Fprintln(stderr, "could not close scenario file")
		return 2
	}
	hurl, err := prepareHurl(s.HurlFile, *scenarioPath, *allowHurl)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *reportPath != "" && (sameFile(*scenarioPath, *reportPath) || hurl != nil && sameFile(hurl.file, *reportPath)) {
		fmt.Fprintln(stderr, "report must not overwrite an input file")
		return 2
	}
	started := time.Now()
	nativeScenario := s
	nativeScenario.HurlFile = ""
	report, runErr := hookcheck.Run(ctx, nativeScenario, hookcheck.Options{BaseURL: *baseURL})
	if runErr != nil && report.Scenario == "" {
		fmt.Fprintf(stderr, "could not run scenario: %v\n", runErr)
		return 2
	}
	if hurl != nil {
		result := hookcheck.CheckResult{Name: "Hurl", Outcome: "not_run"}
		if runErr == nil {
			result = hurl.run(ctx, *baseURL)
		}
		report.Checks = append(report.Checks, result)
		report.Passed = report.Passed && result.Outcome == "passed"
		if ctx.Err() != nil {
			runErr = ctx.Err()
		}
	}
	report.DurationMS = time.Since(started).Milliseconds()
	if *reportPath != "" {
		if err := writeReport(*reportPath, report); err != nil {
			fmt.Fprintln(stderr, "could not write JSON report")
			return 2
		}
	}
	if err := printSummary(stdout, report); err != nil {
		fmt.Fprintln(stderr, "could not write summary")
		return 2
	}
	if runErr != nil {
		return 130
	}
	if hurl != nil && report.Checks[len(report.Checks)-1].Outcome == "external_error" {
		return 2
	}
	if !report.Passed {
		return 1
	}
	return 0
}

func sameFile(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil && absA == absB {
		return true
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func printSummary(w io.Writer, report hookcheck.Report) error {
	status := "failed"
	if report.Passed {
		status = "passed"
	}
	passed := 0
	for _, a := range report.Attempts {
		if a.Outcome == "passed" {
			passed++
		}
	}
	if _, err := fmt.Fprintf(w, "%s: %s\nSeed: %d | deliveries: %d/%d passed | checks: %d | elapsed: %dms\n", status, report.Scenario, report.Seed, passed, len(report.Attempts), len(report.Checks), report.DurationMS); err != nil {
		return err
	}
	shown := 0
	for _, a := range report.Attempts {
		if a.Outcome == "passed" {
			continue
		}
		if shown >= 20 {
			break
		}
		shown++
		if _, err := fmt.Fprintf(w, "  %s #%d %s/%s copy=%d HTTP=%d: %s\n", a.Outcome, a.ID, a.Step, a.Event, a.Copy, a.Status, a.Message); err != nil {
			return err
		}
	}
	for _, c := range report.Checks {
		if _, err := fmt.Fprintf(w, "  check %q: %s (polls=%d) %s\n", c.Name, c.Outcome, c.Polls, c.Message); err != nil {
			return err
		}
	}
	return nil
}
