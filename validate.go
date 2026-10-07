package hookcheck

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const maxDeliveries = 10000

var headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// Validate checks resource bounds and contracts without changing the scenario.
func (s Scenario) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported scenario version: %d", s.Version)
	}
	if !validName(s.Name) {
		return fmt.Errorf("scenario name must be 1-100 printable characters")
	}
	if _, err := duration(s.Timeout, 5*time.Second); err != nil {
		return fmt.Errorf("timeout: %w", err)
	}
	if err := validateHeaders(s.Headers); err != nil {
		return err
	}
	if s.Signing != nil {
		if err := validateHeaders(map[string]string{s.Signing.Header: ""}); err != nil {
			return fmt.Errorf("signing: %w", err)
		}
		if strings.TrimSpace(s.Signing.SecretEnv) == "" || strings.ContainsAny(s.Signing.SecretEnv, "=\x00") {
			return fmt.Errorf("signing requires a valid secret_env name")
		}
	}
	if len(s.Steps) == 0 || len(s.Steps) > 1000 {
		return fmt.Errorf("scenario needs 1-1000 steps")
	}
	seen := map[string]bool{}
	total := 0
	for i, step := range s.Steps {
		if !validName(step.Name) || seen[step.Name] {
			return fmt.Errorf("step %d needs a unique printable name", i+1)
		}
		seen[step.Name] = true
		if err := validatePath(step.Path); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
		if step.Concurrency < 0 || step.Concurrency > 64 {
			return fmt.Errorf("step %q: concurrency must be 1-64 or omitted", step.Name)
		}
		if _, err := duration(step.Timeout, 5*time.Second); err != nil {
			return fmt.Errorf("step %q timeout: %w", step.Name, err)
		}
		if !validStatus(step.ExpectStatus) {
			return fmt.Errorf("step %q: invalid expect_status", step.Name)
		}
		if len(step.Events) == 0 {
			return fmt.Errorf("step %q needs events", step.Name)
		}
		names := map[string]bool{}
		for _, event := range step.Events {
			if !validName(event.Name) || names[event.Name] {
				return fmt.Errorf("step %q needs unique printable event names", step.Name)
			}
			names[event.Name] = true
			if !json.Valid(event.Body) {
				return fmt.Errorf("event %q needs a JSON body", event.Name)
			}
			if event.Repeat < 0 || event.Repeat > 1000 {
				return fmt.Errorf("event %q: repeat must be 1-1000 or omitted", event.Name)
			}
			if err := validateHeaders(event.Headers); err != nil {
				return fmt.Errorf("event %q: %w", event.Name, err)
			}
			total += defaultInt(event.Repeat, 1)
			if total > maxDeliveries {
				return fmt.Errorf("scenario exceeds %d deliveries", maxDeliveries)
			}
		}
	}
	seen = map[string]bool{}
	if len(s.Checks) > 1000 {
		return fmt.Errorf("scenario exceeds 1000 checks")
	}
	for _, check := range s.Checks {
		if !validName(check.Name) || seen[check.Name] {
			return fmt.Errorf("checks need unique printable names")
		}
		seen[check.Name] = true
		if err := validatePath(check.Path); err != nil {
			return fmt.Errorf("check %q: %w", check.Name, err)
		}
		if !validStatus(check.ExpectStatus) {
			return fmt.Errorf("check %q: invalid expect_status", check.Name)
		}
		if check.Within == "" && check.Interval != "" {
			return fmt.Errorf("check %q: interval requires within", check.Name)
		}
		if _, err := duration(check.Within, 0); err != nil {
			return fmt.Errorf("check %q within: %w", check.Name, err)
		}
		if _, err := duration(check.Interval, 50*time.Millisecond); err != nil {
			return fmt.Errorf("check %q interval: %w", check.Name, err)
		}
		for field, expected := range check.Fields {
			if !json.Valid(expected) {
				return fmt.Errorf("check %q field %q needs a JSON value", check.Name, field)
			}
		}
	}
	return nil
}

func duration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 || d > time.Hour {
		return 0, fmt.Errorf("duration must be positive and at most 1h")
	}
	return d, nil
}

func validName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 100 && !strings.ContainsFunc(name, unicode.IsControl)
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func validStatus(status int) bool { return status == 0 || status >= 100 && status <= 599 }

func validatePath(path string) error {
	u, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "#") || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return fmt.Errorf("path must be an origin-relative HTTP path without a fragment")
	}
	return nil
}

func validateHeaders(headers map[string]string) error {
	seen := map[string]bool{}
	for name, value := range headers {
		if !headerName.MatchString(name) {
			return fmt.Errorf("invalid HTTP header name")
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate HTTP header name")
		}
		seen[key] = true
		switch key {
		case "host", "content-length", "transfer-encoding", "connection":
			return fmt.Errorf("transport-managed HTTP header is not allowed")
		}
		for _, char := range value {
			if char < 32 && char != '\t' || char == 127 {
				return fmt.Errorf("invalid HTTP header value")
			}
		}
	}
	return nil
}
