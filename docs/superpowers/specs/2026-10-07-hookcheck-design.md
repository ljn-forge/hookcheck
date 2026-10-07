# Hookcheck v0.1 design

Approved direction: a Go CLI for repeatable webhook reliability tests, prioritizing code quality, demonstrable behavior, and a small maintainable surface.

## Product contract

`hookcheck run --scenario FILE --base-url URL [--report FILE]` loads a strict, versioned JSON scenario. Sequential steps dispatch JSON events with explicit repeats, bounded concurrency and optional seeded shuffle. Serial steps preserve their dispatch order; concurrent arrival order is intentionally not guaranteed. Every delivery records its outcome independently. HTTP success alone does not prove business correctness: named checks query final state and compare specified top-level JSON fields. Polling is opt-in and bounded for eventually consistent systems.

Exit codes: 0 passed; 1 test failure; 2 invalid configuration, dependency, or report I/O; 130 interrupted. Cancellation yields a partial report and waits for workers to exit. No automatic webhook retry; only explicit repeats in the scenario cause additional sends. Responses are size limited and redirects disabled. Report JSON excludes request/response payloads, URLs, headers and secret values. HMAC-SHA256 signs the exact outgoing body using an environment secret; this generic format is not a provider-specific signature adapter.

An optional Hurl file handles complex HTTP assertions by invoking the existing Hurl CLI under the same cancellation context. The path resolves relative to the scenario file. Basic field equality uses encoding/json and reflect.DeepEqual; no custom query language. Hurl requires explicit --allow-hurl because files can perform HTTP operations independently of the scenario target.

## Architecture

- Root `hookcheck` package: scenario loading/validation, bounded execution, assertions and report types. No CLI, process signals or filesystem report writes in the execution core. Run rejects nonempty HurlFile; the CLI prepares the external check and passes an execution copy with that field cleared.
- `internal/cli`: flags, signals supplied by main, human summary, atomically replaced report file, optional Hurl integration.
- `internal/demo`: signed in-memory payment fixture, selectable broken/fixed mode, synchronized state. Business-order idempotency prevents two distinct success events from granting twice; older state events cannot roll back paid status.
- `cmd/`: thin entrypoints.

Use the standard library for HTTP, JSON, HMAC, cancellation, flags and subprocesses. Do not copy employer-specific payment code or publish account credentials.

## Scope and limits

JSON events only; JSON field equality checks are top-level and structural. Limits: 1 MiB scenario, 1 MiB response, 10,000 deliveries, 64 workers per step, 1,000 repeats per event. Network timeouts and explicit repeats supported; connection-level chaos remains Toxiproxy's responsibility. No database polling language, hosted service, UI, real merchant integrations or automatic publication in v0.1. Scenarios are active test inputs, not passive documents; run them against authorized test services.

## Existing solutions comparison

Inspected local Go draft API patterns: request context, size bounds, httptest and idempotency tests. No standalone scenario runner found. Hurl (Apache-2.0, Rust/libcurl) already supplies mature HTTP queries and reports: optional subprocess reuse avoids duplicating its query language. Toxiproxy (MIT, Go) supplies network faults: keep it an optional external tool. Venom (Apache-2.0, Go) provides general executors/assertions: its general runner dependency is broader than this small webhook-focused core; prefer composition over embedding it. All three have published repositories and releases; no claim of an exhaustive security audit. Dependency compatibility risk is kept external to the zero-third-party-dependency Go binary.

Sources checked 2026-10-07:
- https://github.com/Orange-OpenSource/hurl
- https://github.com/Shopify/toxiproxy
- https://github.com/ovh/venom
- https://hurl.dev/docs/manual.html (exit codes 0 success, 1/2 configuration, 3 runtime, 4 assertion)
- https://github.com/Orange-OpenSource/hurl/releases/tag/8.0.1 (actual integration release)
- https://github.com/actions/checkout/commit/3d3c42e5aac5ba805825da76410c181273ba90b1
- https://github.com/actions/setup-go/commit/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e

## Acceptance evidence

The same scenario fails on the broken demo and passes on the fixed demo. Real HTTP tests verify exact signed bytes, business idempotency, out-of-order state, bounded concurrency, timeout after server acceptance, cancellation and polling. Decoder fuzzing, race tests, vet, build and executable CLI smoke checks complete before delivery.
