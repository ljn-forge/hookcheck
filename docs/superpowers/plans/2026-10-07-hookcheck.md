# Hookcheck Implementation Plan

**Goal:** Deliver the approved webhook reliability CLI with executable broken/fixed examples and verified cancellation/concurrency behavior.

**Architecture:** A standard-library Go core compiles a strict scenario into a delivery plan, executes bounded steps, then verifies business state. CLI owns files and external Hurl invocation; the demo is a separate HTTP fixture.

**Tech Stack:** Go 1.25+, net/http, encoding/json, crypto/hmac, testing/httptest; optional Hurl CLI.

## Execution sequence

- [x] Scenario contract (`scenario.go`, `scenario_test.go`): define version/name/seed/timeout, steps/events and checks; write invalid-input table and fuzz decoder before implementation. Verify with `go test -run 'TestScenario|FuzzLoad' .`.
- [x] Execution core (`runner.go`, `http.go`, `check.go`, `report.go`, `runner_test.go`): first write actual HTTP tests for duplicate delivery, stable seeded plan, raw-body HMAC, bounded concurrency, no redirect, oversized response, timeout, cancellation, final-state assertions and bounded polling. Run red before implementing. Verify with `go test -race .`.
- [x] Demo (`internal/demo/`, `cmd/payment-demo/`): tests distinguish event deduplication from business-order idempotency and protect newer state. Implement signed broken/fixed fixture. Verify with `go test -race ./internal/demo`.
- [x] CLI (`internal/cli/`, `cmd/hookcheck/`): tests cover exit 0/1/2/130, partial report, atomic report permissions, Hurl cancellation/error handling. Implement flags and safe output. Verify with `go test -race ./internal/cli`.
- [x] Open-source handoff: English and Chinese README, MIT license, contributing guide, CI, Makefile and JSON/Hurl examples. Verify broken/fixed examples through compiled executables and actual Hurl 8.0.1.
- [x] Final verification: `gofmt`, `go test -race -cover ./...`, `go vet ./...`, `go build ./...`, bounded fuzz run, whitespace and independent code review. Observed outcomes recorded in `docs/verification.md`.

## Critical test contracts

```go
report, err := Run(ctx, scenario, Options{BaseURL: server.URL})
if err != nil { t.Fatal(err) }
if !report.Passed { t.Fatalf("report: %+v", report) }
```

```go
scenario, err := Load(strings.NewReader(input))
if err == nil { t.Fatal("invalid scenario accepted") }
_ = scenario
```

```go
code := Run(ctx, []string{"run", "--scenario", scenarioPath, "--base-url", server.URL}, &out, &stderr)
if code != wantCode { t.Fatalf("exit=%d want=%d", code, wantCode) }
```

Review checkpoints are progress updates in this session. Scope is already approved; implementation proceeds inline. Remote creation/push is outside this task.
