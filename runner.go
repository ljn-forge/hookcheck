package hookcheck

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Options struct {
	BaseURL   string
	LookupEnv func(string) (string, bool)
}

// Run executes native deliveries and checks, returning a partial report on
// cancellation. Scenarios with HurlFile must be executed by the CLI; Run rejects
// them rather than silently skipping an external assertion.
func Run(ctx context.Context, s Scenario, options Options) (Report, error) {
	if err := s.Validate(); err != nil {
		return Report{}, err
	}
	if s.HurlFile != "" {
		return Report{}, fmt.Errorf("external Hurl checks require the hookcheck CLI")
	}
	base, err := parseBaseURL(options.BaseURL)
	if err != nil {
		return Report{}, err
	}
	lookup := options.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var secret string
	if s.Signing != nil {
		var ok bool
		secret, ok = lookup(s.Signing.SecretEnv)
		if !ok || secret == "" {
			return Report{}, fmt.Errorf("signing secret environment variable is missing or empty")
		}
	}
	transport := http.DefaultTransport
	if original, ok := transport.(*http.Transport); ok {
		owned := original.Clone()
		transport = owned
		defer owned.CloseIdleConnections()
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	started := time.Now()
	report := Report{Version: 1, Scenario: s.Name, Seed: s.Seed, StartedAt: started.UTC(), Attempts: []Attempt{}, Checks: make([]CheckResult, len(s.Checks))}
	for i, check := range s.Checks {
		report.Checks[i] = CheckResult{Name: check.Name, Outcome: "not_run"}
	}
	plans := make([][]delivery, len(s.Steps))
	rng := rand.New(rand.NewSource(s.Seed))
	for i, step := range s.Steps {
		for _, event := range step.Events {
			for copy := 1; copy <= defaultInt(event.Repeat, 1); copy++ {
				plans[i] = append(plans[i], delivery{event: event, copy: copy})
			}
		}
		if step.Shuffle {
			rng.Shuffle(len(plans[i]), func(a, b int) { plans[i][a], plans[i][b] = plans[i][b], plans[i][a] })
		}
		for j := range plans[i] {
			plans[i][j].index = len(report.Attempts)
			report.Attempts = append(report.Attempts, Attempt{ID: len(report.Attempts) + 1, Step: step.Name, Event: plans[i][j].event.Name, Copy: plans[i][j].copy, Outcome: "not_sent"})
		}
	}
	timeout, _ := duration(s.Timeout, 5*time.Second)
	for i, step := range s.Steps {
		if ctx.Err() != nil {
			break
		}
		stepTimeout, _ := duration(step.Timeout, timeout)
		runStep(ctx, client, base, s, secret, step, stepTimeout, plans[i], report.Attempts)
	}
	for i, check := range s.Checks {
		if ctx.Err() != nil {
			break
		}
		report.Checks[i] = runCheck(ctx, client, base, s.Headers, check, timeout)
	}
	report.DurationMS = time.Since(started).Milliseconds()
	runErr := ctx.Err()
	report.Passed = runErr == nil
	for _, a := range report.Attempts {
		report.Passed = report.Passed && a.Outcome == "passed"
	}
	for _, c := range report.Checks {
		report.Passed = report.Passed && c.Outcome == "passed"
	}
	return report, runErr
}

type delivery struct {
	event Event
	copy  int
	index int
}

func runStep(ctx context.Context, client *http.Client, base string, s Scenario, secret string, step Step, timeout time.Duration, plan []delivery, attempts []Attempt) {
	jobs := make(chan delivery)
	var workers sync.WaitGroup
	for i := 0; i < min(defaultInt(step.Concurrency, 1), len(plan)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				start := time.Now()
				at := start.UTC()
				result := request(ctx, client, base+step.Path, http.MethodPost, job.event.Body, s.Headers, job.event.Headers, s.Signing, secret, timeout, step.ExpectStatus)
				a := &attempts[job.index]
				a.StartedAt = &at
				a.DurationMS = time.Since(start).Milliseconds()
				a.Status = result.status
				a.Outcome = result.outcome
				a.Message = result.message
			}
		}()
	}
dispatch:
	for _, job := range plan {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- job:
		}
	}
	close(jobs)
	workers.Wait()
}

func parseBaseURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Opaque != "" {
		return "", fmt.Errorf("base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}
