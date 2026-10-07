package hookcheck

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckWindowExpiryDuringReadAndInterval(t *testing.T) {
	for _, slowBody := range []bool{true, false} {
		s := testScenario(t)
		s.Checks[0].Within = "30ms"
		s.Checks[0].Interval = "1s"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				return
			}
			if slowBody {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			io.WriteString(w, `{"grants":9,"status":"pending"}`)
		}))
		started := time.Now()
		report, err := Run(context.Background(), s, Options{BaseURL: server.URL})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if report.Passed || report.Checks[0].Outcome != "failed" || report.Checks[0].Polls != 1 {
			t.Fatalf("slow body=%t report=%+v", slowBody, report)
		}
		if time.Since(started) > time.Second {
			t.Fatal("check exceeded its window")
		}
	}
}

func TestResponseReadTimeoutAfterHeaders(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Timeout = "30ms"
	s.Steps[0].Events[0].Repeat = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	report, err := Run(context.Background(), s, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.Attempts[0].Outcome != "timeout" || report.Attempts[0].Status != 200 {
		t.Fatalf("report=%+v", report)
	}
}

func TestCancelSingleNonPollingCheck(t *testing.T) {
	s := testScenario(t)
	queried := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			return
		}
		close(queried)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		report Report
		err    error
	}
	done := make(chan result, 1)
	go func() { report, err := Run(ctx, s, Options{BaseURL: server.URL}); done <- result{report, err} }()
	select {
	case <-queried:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || got.report.Passed || got.report.Checks[0].Outcome != "cancelled" {
			t.Fatalf("result=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("check cancellation blocked")
	}
}

func TestResponseExactlyAtLimit(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Steps[0].Events[0].Repeat = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", 1<<20)) }))
	defer server.Close()
	report, err := Run(context.Background(), s, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("boundary rejected: %+v", report)
	}
}

func TestJSONFieldEqualityContracts(t *testing.T) {
	for _, test := range []struct {
		body, expected string
		match          bool
	}{
		{`{"value":1}`, `1`, true},
		{`{"value":1}`, `1.0`, false},
		{`{"value":null}`, `null`, true},
		{`{}`, `null`, false},
		{`{"value":{"b":2,"a":1}}`, `{"a":1,"b":2}`, true},
		{`{"value":[1,2]}`, `[2,1]`, false},
		{`{"value":1} {}`, `1`, false},
		{`null`, `null`, false},
	} {
		err := matchFields([]byte(test.body), map[string]json.RawMessage{"value": json.RawMessage(test.expected)})
		if (err == nil) != test.match {
			t.Fatalf("body=%s expected=%s err=%v", test.body, test.expected, err)
		}
	}
}

func TestScenarioAllowsEncodedHashInPath(t *testing.T) {
	_, err := Load(strings.NewReader(strings.Replace(validScenario, `"/webhooks"`, `"/webhooks%23fragment"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
}

func TestGoAPICannotSilentlySkipExternalChecks(t *testing.T) {
	s := testScenario(t)
	s.HurlFile = "required.hurl"
	if report, err := Run(context.Background(), s, Options{BaseURL: "http://127.0.0.1:1"}); err == nil || report.Passed {
		t.Fatalf("external check silently skipped: %+v err=%v", report, err)
	}
}
