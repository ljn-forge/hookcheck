package hookcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Scenario is a versioned webhook test. Callers must not mutate it during Run.
type Scenario struct {
	Version  int               `json:"version"`
	Name     string            `json:"name"`
	Seed     int64             `json:"seed"`
	Timeout  string            `json:"timeout,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Signing  *Signing          `json:"signing,omitempty"`
	Steps    []Step            `json:"steps"`
	Checks   []Check           `json:"checks,omitempty"`
	HurlFile string            `json:"hurl_file,omitempty"`
}

// Signing uses hex HMAC-SHA256 of the exact outgoing JSON bytes.
type Signing struct {
	Header    string `json:"header"`
	SecretEnv string `json:"secret_env"`
}

type Step struct {
	Name         string  `json:"name"`
	Path         string  `json:"path"`
	Concurrency  int     `json:"concurrency,omitempty"`
	Shuffle      bool    `json:"shuffle,omitempty"`
	Timeout      string  `json:"timeout,omitempty"`
	ExpectStatus int     `json:"expect_status,omitempty"`
	Events       []Event `json:"events"`
}

type Event struct {
	Name    string            `json:"name"`
	Body    json.RawMessage   `json:"body"`
	Repeat  int               `json:"repeat,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type Check struct {
	Name         string                     `json:"name"`
	Path         string                     `json:"path"`
	ExpectStatus int                        `json:"expect_status,omitempty"`
	Fields       map[string]json.RawMessage `json:"fields,omitempty"`
	Within       string                     `json:"within,omitempty"`
	Interval     string                     `json:"interval,omitempty"`
}

// Load reads a strict versioned JSON scenario.
func Load(r io.Reader) (Scenario, error) {
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario: %w", err)
	}
	if len(data) > limit {
		return Scenario{}, fmt.Errorf("scenario exceeds %d bytes", limit)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var s Scenario
	if err := decoder.Decode(&s); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Scenario{}, fmt.Errorf("scenario must contain one JSON document")
	}
	if err := s.Validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}
