package hookcheck

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testScenario(t *testing.T) Scenario {
	t.Helper()
	s, err := Load(strings.NewReader(validScenario))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunDuplicateEventsAndBusinessCheck(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			calls.Add(1)
			w.WriteHeader(204)
			return
		}
		io.WriteString(w, `{"grants":1,"status":"paid"}`)
	}))
	defer server.Close()
	report, err := Run(context.Background(), testScenario(t), Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Attempts) != 3 || calls.Load() != 3 || report.Checks[0].Polls != 1 {
		t.Fatalf("report=%+v calls=%d", report, calls.Load())
	}
	for i, a := range report.Attempts {
		if a.ID != i+1 || a.Copy != i+1 || a.Status != 204 || a.Outcome != "passed" {
			t.Fatalf("attempt=%+v", a)
		}
	}
}

func TestRunChecksBusinessStateAfterSuccessfulHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"grants":3,"status":"pending"}`) }))
	defer server.Close()
	report, err := Run(context.Background(), testScenario(t), Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.Checks[0].Outcome != "failed" || !strings.Contains(report.Checks[0].Message, "grants") {
		t.Fatalf("report=%+v", report)
	}
	for _, a := range report.Attempts {
		if a.Outcome != "passed" {
			t.Fatal("HTTP delivery did not pass")
		}
	}
}

func TestRunSignsExactBodyAndDoesNotLeakSecrets(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Signing = &Signing{Header: "X-Signature", SecretEnv: "TEST_SECRET"}
	s.Steps[0].Events[0].Body = json.RawMessage(`{ "id": "event-1", "secret": "payload-secret" }`)
	s.Headers = map[string]string{"Authorization": "Bearer auth-secret"}
	s.Headers["X-Signature"] = "wrong-scenario-signature"
	s.Steps[0].Events[0].Headers = map[string]string{"x-signature": "wrong-event-signature"}
	var bad atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("signing-secret"))
		mac.Write(body)
		if string(body) != string(s.Steps[0].Events[0].Body) || r.Header.Get("X-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			bad.Store(true)
			w.WriteHeader(401)
		}
	}))
	defer server.Close()
	report, err := Run(context.Background(), s, Options{BaseURL: server.URL, LookupEnv: func(string) (string, bool) { return "signing-secret", true }})
	if err != nil {
		t.Fatal(err)
	}
	if bad.Load() || !report.Passed {
		t.Fatalf("report=%+v", report)
	}
	b, _ := json.Marshal(report)
	for _, secret := range []string{"signing-secret", "payload-secret", "auth-secret", server.URL} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("report leaked %q", secret)
		}
	}
}

func TestRunConcurrencyBoundAndCancellation(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Steps[0].Concurrency = 3
	s.Steps[0].Events[0].Repeat = 20
	started := make(chan struct{}, 20)
	var active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
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
	go func() { r, e := Run(ctx, s, Options{BaseURL: server.URL}); done <- result{r, e} }()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case res := <-done:
		if !errors.Is(res.err, context.Canceled) || res.report.Passed || len(res.report.Attempts) != 20 {
			t.Fatalf("result=%+v", res)
		}
		if maximum.Load() != 3 {
			t.Fatalf("maximum=%d", maximum.Load())
		}
		for _, a := range res.report.Attempts {
			if a.Outcome != "cancelled" && a.Outcome != "not_sent" {
				t.Fatalf("unexpected attempt=%+v", a)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop workers")
	}
}

func TestRunSeededOrderAndSequentialSteps(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Steps[0].Shuffle = true
	s.Steps[0].Events = append(s.Steps[0].Events, Event{Name: "older", Body: json.RawMessage(`{"id":"older"}`), Repeat: 2})
	s.Steps = append(s.Steps, Step{Name: "after", Path: "/after", Events: []Event{{Name: "last", Body: json.RawMessage(`{}`)}}})
	var mu sync.Mutex
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(b))
		mu.Unlock()
	}))
	defer server.Close()
	var previous []string
	for i := 0; i < 2; i++ {
		r, err := Run(context.Background(), s, Options{BaseURL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Passed || r.Attempts[len(r.Attempts)-1].Step != "after" {
			t.Fatalf("report=%+v", r)
		}
		mu.Lock()
		current := append([]string(nil), received...)
		received = nil
		mu.Unlock()
		if i == 1 && !reflect.DeepEqual(previous, current) {
			t.Fatalf("seed changed order: %v %v", previous, current)
		}
		previous = current
	}
}

func TestRunTimeoutAfterServerAcceptanceDoesNotRetry(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Timeout = "20ms"
	s.Steps[0].Events[0].Repeat = 1
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	r, err := Run(context.Background(), s, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || calls.Load() != 1 || r.Attempts[0].Outcome != "timeout" {
		t.Fatalf("report=%+v calls=%d", r, calls.Load())
	}
}

func TestRunDoesNotFollowRedirects(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	var redirects atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	r, err := Run(context.Background(), s, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || redirects.Load() != 0 || r.Attempts[0].Status != 307 {
		t.Fatalf("report=%+v redirected=%d", r, redirects.Load())
	}
}

func TestRunPollingAndLargeJSONNumbers(t *testing.T) {
	s := testScenario(t)
	s.Checks[0].Within = "500ms"
	s.Checks[0].Interval = "1ms"
	s.Checks[0].Fields = map[string]json.RawMessage{"id": json.RawMessage(`9007199254740993`)}
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			return
		}
		if polls.Add(1) < 3 {
			io.WriteString(w, `{"id":9007199254740992}`)
			return
		}
		io.WriteString(w, `{"id":9007199254740993}`)
	}))
	defer server.Close()
	r, err := Run(context.Background(), s, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Passed || r.Checks[0].Polls != 3 {
		t.Fatalf("report=%+v", r)
	}
}

func TestRunRejectsOversizedResponse(t *testing.T) {
	s := testScenario(t)
	s.Checks = nil
	s.Steps[0].Events[0].Repeat = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", (1<<20)+1)) }))
	defer server.Close()
	r, err := Run(context.Background(), s, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || r.Attempts[0].Outcome != "response_too_large" {
		t.Fatalf("report=%+v", r)
	}
}

func TestRunInvalidOptionsMakeNoRequests(t *testing.T) {
	s := testScenario(t)
	for _, base := range []string{"", "ftp://example.com", "http://user:secret@example.com", "http://example.com?secret=a", "http://example.com/prefix"} {
		if _, err := Run(context.Background(), s, Options{BaseURL: base}); err == nil {
			t.Fatalf("accepted %q", base)
		}
	}
	s.Signing = &Signing{Header: "X-Signature", SecretEnv: "ABSENT"}
	if _, err := Run(context.Background(), s, Options{BaseURL: "http://localhost", LookupEnv: func(string) (string, bool) { return "", false }}); err == nil {
		t.Fatal("missing secret accepted")
	}
}

func TestRunDoesNotTransparentlyRetryPOST(t *testing.T) {
	for _, header := range []string{"Idempotency-Key", "X-Idempotency-Key"} {
		t.Run(header, func(t *testing.T) {
			s := testScenario(t)
			s.Checks = nil
			s.Steps[0].Events[0].Repeat = 2
			s.Headers = map[string]string{header: "event-1"}
			var accepted atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				if accepted.Add(1) == 2 {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					connection.Close()
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			report, err := Run(context.Background(), s, Options{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if accepted.Load() != 2 {
				t.Fatalf("accepted %d requests for 2 planned deliveries", accepted.Load())
			}
			if report.Passed || report.Attempts[1].Outcome != "transport_error" {
				t.Fatalf("report=%+v", report)
			}
		})
	}
}
