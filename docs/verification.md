# Verification record

Observed locally on 2026-10-07 using Go 1.26.2, darwin/arm64. This record describes local evidence; the included Linux/macOS Go 1.25/1.26 CI matrix has not yet run remotely.

## Final checks

`make check` passed:

- Formatting: no files reported by `gofmt -l .`.
- `go test -race -coverprofile=coverage.out -timeout 60s ./...`: passed.
- `go vet ./...`: passed.
- `go build ./...`: passed.

Statement coverage: execution core 89.8%, CLI 83.3%, demo 85.3%, all packages combined 81.6%. Command entrypoints are exercised by compiled-process smoke tests rather than Go's unit coverage instrumentation. Coverage is diagnostic, not proof of correctness.

`make fuzz` passed after 371,149 parser executions over approximately 11 seconds, with two workers and no failing input. The decoder rejects unknown fields, invalid contracts, trailing JSON and oversized scenarios.

## Compiled-process smoke

Built both executables with `-trimpath`, then ran `scripts/smoke.py` against fresh processes on ephemeral IPv4 loopback ports:

```text
broken/native/scenario.json: exit 1; 6 deliveries, 2 checks
fixed/native/scenario.json: exit 0; 6 deliveries, 2 checks
fixed/native/timeout.json: exit 1; 2 deliveries, 1 checks
interrupted process: exit 130; partial report retained
broken/hurl/scenario-hurl.json: exit 1; 6 deliveries, 3 checks
fixed/hurl/scenario-hurl.json: exit 0; 6 deliveries, 3 checks
```

The timeout example observed one timed-out delivery and a passing final-state query after the payment had committed. The interrupt example used SIGTERM while acknowledgement was delayed, then verified readable partial JSON, cancelled attempts and unsent later events. Report files had private permissions and contained neither the signing secret nor target origin.

Actual Hurl 8.0.1 was downloaded to a temporary directory from its [official release](https://github.com/Orange-OpenSource/hurl/releases/tag/8.0.1), not installed globally. Its archive matched the [published asset digest](https://github.com/Orange-OpenSource/hurl/releases/expanded_assets/8.0.1):

```text
hurl-8.0.1-aarch64-apple-darwin.tar.gz
b57928e246617df73cb1b2157f31f507dcbde6ae12e828cc53dde0e40e05bbbb
```

Repeat native checks with `make smoke`; it also runs Hurl checks whenever `hurl` is on PATH. The observed Hurl run used the temporary release binary on PATH.

## Review and regressions

Independent read-only code review found and led to fixes for:

1. Transparent POST retries on reused connections with idempotency headers. A red HTTP test observed three accepted POSTs for two planned attempts. Clearing GetBody prevents replay; regression cases cover both Idempotency-Key and X-Idempotency-Key.
2. Fragment-containing paths being accepted but silently truncated by the request constructor. Raw fragments are rejected, encoded `%23` remains valid.
3. External wrapper children holding output pipes after cancellation. Null output, bounded waiting and owned Unix process groups prevent the hang.
4. Report elapsed time excluding Hurl. It now includes the external check.
5. Terminal paid state depending on event arrival order. The fixture now grants once and preserves paid in either order, independent of event version ordering.
6. The Go API silently skipping external Hurl checks. Run now rejects such scenarios before network effects; the CLI prepares and executes them explicitly.

Additional HTTP regressions cover exact body signing and header precedence, business checks after successful delivery, bounded workers, cancellation during delivery and non-polling checks, request timeout after headers, polling expiry during response reads and interval waits, redirect refusal, exact/oversized response bounds, large integers and numeric token comparison.

Initial attempts under the restricted shell failed to bind loopback ports. The same unmodified checks were rerun with local-listener permission and passed. No tests were changed to bypass those failures. An earlier cancellation fixture needed its POST body drained to observe client disconnect; the corrected fixture verifies both client return and server shutdown.

## Scope

This is a local v0.1 project and an in-memory test fixture. No real payment provider, production service, external publication, remote GitHub repository or deployed CI run is represented by these results. Generic HMAC is not a provider signature adapter. Network chaos remains an external Toxiproxy use case; it is not implemented or claimed tested here.
