package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ljn-forge/hookcheck"
	"github.com/ljn-forge/hookcheck/internal/demo"
)

const scenarioJSON = `{"version":1,"name":"cli-test","seed":1,"steps":[{"name":"event","path":"/webhooks","events":[{"name":"event-1","body":{},"repeat":3}]}],"checks":[{"name":"business","path":"/state","fields":{"grants":1}}]}`

func scenarioFile(t *testing.T, data string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestCLIExitCodesAndReport(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		code int
	}{{"passed", `{"grants":1}`, 0}, {"failed", `{"grants":3}`, 1}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, test.body) }))
			defer server.Close()
			file := scenarioFile(t, scenarioJSON)
			destination := filepath.Join(t.TempDir(), "report.json")
			var out, stderr bytes.Buffer
			code := Run(context.Background(), []string{"run", "--scenario", file, "--base-url", server.URL, "--report", destination}, &out, &stderr)
			if code != test.code {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			b, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			var r hookcheck.Report
			if err := json.Unmarshal(b, &r); err != nil {
				t.Fatal(err)
			}
			if r.Passed != (test.code == 0) || len(r.Attempts) != 3 || !strings.Contains(out.String(), test.name) {
				t.Fatalf("report=%+v output=%s", r, out.String())
			}
			info, _ := os.Stat(destination)
			if info.Mode().Perm()&0077 != 0 {
				t.Fatal("report is not private")
			}
		})
	}
}

func TestCLIInvalidConfigurationAndCancelledPartialReport(t *testing.T) {
	file := scenarioFile(t, scenarioJSON)
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"run", "--scenario", file, "--base-url", "ftp://localhost"}, &out, &stderr); code != 2 {
		t.Fatalf("exit=%d", code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := filepath.Join(t.TempDir(), "partial.json")
	if code := Run(ctx, []string{"run", "--scenario", file, "--base-url", "http://127.0.0.1:1", "--report", destination}, &out, &stderr); code != 130 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	b, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var report hookcheck.Report
	json.Unmarshal(b, &report)
	if report.Passed || report.Attempts[0].Outcome != "not_sent" {
		t.Fatalf("partial=%+v", report)
	}
}

func TestCLIProtectsScenarioAndChecksReportWriteErrors(t *testing.T) {
	file := scenarioFile(t, scenarioJSON)
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"run", "--scenario", file, "--base-url", "http://localhost", "--report", file}, &out, &stderr); code != 2 {
		t.Fatalf("exit=%d", code)
	}
	b, _ := os.ReadFile(file)
	if string(b) != scenarioJSON {
		t.Fatal("scenario overwritten")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"grants":1}`) }))
	defer server.Close()
	if code := Run(context.Background(), []string{"run", "--scenario", file, "--base-url", server.URL, "--report", t.TempDir()}, &out, &stderr); code != 2 {
		t.Fatalf("report write exit=%d", code)
	}
}

func TestCLIUsage(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"run", "--help"}, {"version"}} {
		var out, err bytes.Buffer
		if code := Run(context.Background(), args, &out, &err); code != 0 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"run"}, {"run", "--unexpected"}} {
		if code := Run(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCLIOutputErrors(t *testing.T) {
	if code := Run(context.Background(), []string{"version"}, failWriter{}, io.Discard); code != 2 {
		t.Fatalf("exit=%d", code)
	}
}

func TestCLIExampleDistinguishesBrokenAndFixedDemo(t *testing.T) {
	file := filepath.Join("..", "..", "examples", "payment", "scenario.json")
	for _, broken := range []bool{true, false} {
		h, err := demo.New(demo.Config{Secret: "demo-secret", Broken: broken})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(h)
		t.Setenv("HOOKCHECK_DEMO_SECRET", "demo-secret")
		var out, stderr bytes.Buffer
		code := Run(context.Background(), []string{"run", "--scenario", file, "--base-url", server.URL}, &out, &stderr)
		server.Close()
		want := 0
		if broken {
			want = 1
		}
		if code != want {
			t.Fatalf("broken=%t exit=%d stderr=%s out=%s", broken, code, stderr.String(), out.String())
		}
	}
}

func TestCLIHurlOptInAndProcessResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"grants":1}`) }))
	defer server.Close()
	data := strings.TrimSuffix(scenarioJSON, "}") + `,"hurl_file":"state.hurl"}`
	file := scenarioFile(t, data)
	if err := os.WriteFile(filepath.Join(filepath.Dir(file), "state.hurl"), []byte("GET {{base_url}}/state\nHTTP 200\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"run", "--scenario", file, "--base-url", server.URL}
	if code := Run(context.Background(), args, &out, &stderr); code != 2 {
		t.Fatalf("Hurl without opt-in exit=%d", code)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	args = append(args, "--allow-hurl")
	if code := Run(context.Background(), args, &out, &stderr); code != 2 {
		t.Fatalf("missing Hurl exit=%d", code)
	}
	for _, test := range []struct{ external, expected int }{{0, 0}, {1, 2}, {2, 2}, {3, 1}, {4, 1}, {127, 2}} {
		script := fmt.Sprintf("#!/bin/sh\nprintf 'external-secret'\nexit %d\n", test.external)
		if err := os.WriteFile(filepath.Join(bin, "hurl"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		stderr.Reset()
		if got := Run(context.Background(), args, &out, &stderr); got != test.expected {
			t.Fatalf("Hurl external=%d exit=%d want=%d stderr=%s", test.external, got, test.expected, stderr.String())
		}
		if strings.Contains(out.String()+stderr.String(), "external-secret") {
			t.Fatal("Hurl output leaked")
		}
	}
}

func TestHurlCancellationStopsWaitingForWrapperChildren(t *testing.T) {
	bin := t.TempDir()
	script := filepath.Join(bin, "hurl")
	marker := filepath.Join(bin, "started")
	content := "#!/bin/sh\n/bin/sleep 3 &\nprintf ready > '" + marker + "'\nwait\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan hookcheck.CheckResult, 1)
	go func() {
		done <- (&hurlCheck{binary: script, file: filepath.Join(bin, "state.hurl")}).run(ctx, "http://localhost")
	}()
	deadline := time.After(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("wrapper did not start")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case result := <-done:
		if result.Outcome != "cancelled" {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Hurl cancellation waited for child-held pipes")
	}
}

func TestCLIReportIncludesHurlDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"grants":1}`) }))
	defer server.Close()
	file := scenarioFile(t, strings.TrimSuffix(scenarioJSON, "}")+`,"hurl_file":"state.hurl"}`)
	os.WriteFile(filepath.Join(filepath.Dir(file), "state.hurl"), []byte("GET {{base_url}}/state\nHTTP 200\n"), 0600)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "hurl"), []byte("#!/bin/sh\n/bin/sleep 0.1\nexit 0\n"), 0700)
	t.Setenv("PATH", bin)
	reportFile := filepath.Join(t.TempDir(), "report.json")
	code := Run(context.Background(), []string{"run", "--scenario", file, "--base-url", server.URL, "--allow-hurl", "--report", reportFile}, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	b, err := os.ReadFile(reportFile)
	if err != nil {
		t.Fatal(err)
	}
	var report hookcheck.Report
	json.Unmarshal(b, &report)
	if report.DurationMS < 100 {
		t.Fatalf("Hurl omitted from total elapsed: %+v", report)
	}
}
