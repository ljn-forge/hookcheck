# Hookcheck

Test whether your webhook handler remains correct when events repeat, arrive late, or race.

[中文说明](README.zh-CN.md)

An HTTP 200 proves that an endpoint responded. Hookcheck also checks the resulting business state: one grant per order, no stale-state rollback, and explicit failure when an acknowledgement is uncertain.

## Try the payment example

Requires Go 1.25+. The core binary has no third-party Go dependencies. Initial supported platforms: macOS and Linux.

Install the CLI directly:

```sh
go install github.com/ljn-forge/hookcheck/cmd/hookcheck@latest
```

To run the included demo, clone [the repository](https://github.com/ljn-forge/hookcheck) and build both executables:

```sh
git clone https://github.com/ljn-forge/hookcheck.git
cd hookcheck
```

```sh
make build
export HOOKCHECK_DEMO_SECRET=example-only-secret
./bin/payment-demo --mode broken
```

In another terminal, from the same project directory:

```sh
export HOOKCHECK_DEMO_SECRET=example-only-secret
./bin/hookcheck run \
  --scenario examples/payment/scenario.json \
  --base-url http://127.0.0.1:8080 \
  --report broken-report.json
```

All six deliveries succeed at HTTP level. The test exits **1** because four paid deliveries grant four times and the late pending event rolls the state back.

```text
failed: payment idempotency and stale events
Seed: 42 | deliveries: 6/6 passed | checks: 2 | elapsed: ...ms
  check "one grant per order": failed (polls=1) field "grants" did not match
  check "paid state survives stale event": failed (polls=1) field "status" did not match
```

Stop the demo with Ctrl+C, restart it with `--mode fixed`, and run the same command. It exits **0**: repeated event IDs and distinct events for one order grant once; paid is terminal in this two-state fixture.

`make smoke` automates both modes and the timeout example using compiled executables and ephemeral loopback ports. It requires Python 3 only for the test harness.

## Write a scenario

```json
{
  "version": 1,
  "name": "duplicate payment",
  "seed": 42,
  "timeout": "2s",
  "steps": [
    {
      "name": "deliver paid event",
      "path": "/webhooks",
      "concurrency": 3,
      "expect_status": 204,
      "events": [
        {"name": "paid", "repeat": 3, "body": {"id": "paid-1"}}
      ]
    }
  ],
  "checks": [
    {"name": "one grant", "path": "/orders/one", "fields": {"grants": 1}}
  ]
}
```

Point this scenario at your own test service implementing those endpoints. Steps execute sequentially and each waits for its deliveries before the next begins. Events execute in listed order when `concurrency` is 1. A repeat copies the exact same body and headers. There are **no automatic webhook retries**, including transparent POST replay by Go's HTTP transport.

Set `shuffle: true` on a step to shuffle its expanded events using the scenario seed. The seed reproduces the planned order with the same binary and scenario. Concurrent scheduling, network arrival order, timing and target state remain nondeterministic. Use serial steps to deliberately deliver an older event after a newer one. Each report attempt has a plan ID, event name and copy number; IDs describe the plan, while timestamps describe actual starts.

### Configuration reference

| Field | Default / contract |
| --- | --- |
| `version` | Required, `1` |
| `name` | Required printable name, at most 100 UTF-8 bytes |
| `seed` | `0`; applies to seeded step shuffle |
| `timeout` | `5s`; bounds each request, including reading its response |
| `headers` | Scenario-wide headers, applied to deliveries and GET checks |
| `signing` | Optional `header` and `secret_env` |
| `steps[].name`, `path`, `events` | Required; unique step names |
| `steps[].concurrency` | `1`, maximum `64` |
| `steps[].shuffle` | `false` |
| `steps[].timeout` | Inherits scenario timeout |
| `steps[].expect_status` | Any 2xx; explicit value requires exact equality |
| `events[].name`, `body` | Required; unique names within each step, body is a JSON value |
| `events[].repeat` | `1`, maximum `1000` |
| `events[].headers` | Overrides scenario headers for that event |
| `checks[].name`, `path` | Required; unique check names |
| `checks[].expect_status` | `200` |
| `checks[].fields` | Exact structural equality for selected top-level JSON fields |
| `checks[].within` | Omitted: one GET; set a positive duration for bounded polling |
| `checks[].interval` | `50ms` when polling; requires `within` |
| `hurl_file` | Optional external Hurl file; requires `--allow-hurl` |

Unknown fields and trailing JSON documents are rejected. Zero-valued `repeat`, `concurrency` and `expect_status` mean their defaults. Durations are positive and at most one hour. Scenarios are limited to 1 MiB, 1,000 steps/checks and 10,000 total deliveries. Responses are limited to 1 MiB. The base URL is an HTTP(S) origin; paths start with `/`, may include a query, and cannot contain raw fragments or select another origin. Redirects are disabled. Transport-managed headers cannot be set manually.

Field comparison ignores object key order, preserves array order, distinguishes missing fields from null, and preserves large integer precision. Numeric JSON tokens compare exactly: `1` and `1.0` differ. Values within a selected object must match fully; this is not a JSONPath or recursive subset language. An empty `fields` map checks only HTTP status. Scenarios without checks verify delivery outcomes only.

Polling applies to **GET business checks only**, never to delivery POSTs. Its deadline also constrains any in-flight request. Checks run after deliveries even when delivery requests fail, which makes uncertain sends diagnosable.

### Generic signing

```json
"signing": {
  "header": "X-Hookcheck-Signature",
  "secret_env": "HOOKCHECK_SECRET"
}
```

The header receives lowercase hex HMAC-SHA256 of the exact JSON body bytes stored in the scenario. Signing overrides any value for that header after applying event headers. Secrets are loaded once per run from the environment. This is a generic test format, not Stripe/PayPal signature compatibility. Signed GET checks are not provided; use headers or Hurl for query authentication.

### An accepted event with a lost acknowledgement

Start the fixed demo with `--ack-delay 200ms`, then run `examples/payment/timeout.json`. The delivery times out after 20ms, but the business check observes one grant. The run still fails: a correct state query does not turn an uncertain delivery acknowledgement into HTTP success. Hookcheck never blindly resends it.

### Reuse Hurl for complex HTTP assertions

[Hurl](https://github.com/Orange-OpenSource/hurl) supplies HTTP queries, JSONPath, header assertions and its own reports. Install Hurl separately, then:

```sh
./bin/hookcheck run \
  --scenario examples/payment/scenario-hurl.json \
  --base-url http://127.0.0.1:8080 \
  --allow-hurl
```

The file resolves relative to the scenario file. Hookcheck passes `base_url` through Hurl's `--variable` argument without a shell, inherits the environment, and enforces a 30-second total Hurl limit. On macOS/Linux, cancellation terminates its process group. Hurl output is suppressed because it can contain secrets; run the file directly for detailed failures:

```sh
hurl --test --variable base_url=http://127.0.0.1:8080 examples/payment/state.hurl
```

Hurl files are trusted executable test inputs: they can address other hosts, use their own retry settings, and write their own reports. Hookcheck's native delivery bounds and redirect policy do not apply inside Hurl. Review the file before opting in. Network-level latency and disconnect experiments can separately use [Toxiproxy](https://github.com/Shopify/toxiproxy); Hookcheck does not implement a TCP chaos proxy.

## Reports and CI

| Exit | Meaning |
| --- | --- |
| `0` | All deliveries and checks passed |
| `1` | Delivery, state assertion, or Hurl HTTP/assertion failure |
| `2` | Invalid input, missing dependency, external configuration/process error, or output I/O failure |
| `130` | Execution context cancelled or deadline exceeded |

`--report FILE` replaces the destination atomically with a private JSON file. On cancellation, a partial report identifies unsent deliveries and unrun checks. It records metadata, status, timing and assertion field names; it excludes bodies, headers, URLs and secret values. Keep sensitive values out of scenario/event/check names as those are user-provided report metadata. A report cannot overwrite the scenario or Hurl input file.

Use the exit code as a CI gate and keep the report as a private diagnostic artifact. Reports alone do not contain enough data to replay a run: retain the original scenario, binary version, target environment and seed. The included workflow tests Go 1.25/1.26 on Linux/macOS; see [GitHub Actions](https://github.com/ljn-forge/hookcheck/actions) for remote results.

## Development

```sh
make check        # formatting, race tests, vet, build
make fuzz         # bounded parser fuzzing
make smoke        # real compiled CLI + demo services
```

The execution core is the root Go package. Its `Run` API rejects scenarios with `hurl_file` so external assertions cannot be silently skipped; those scenarios require the CLI. CLI/files/processes live in `internal/cli`; the synchronized in-memory fixture lives in `internal/demo`. See [design](docs/superpowers/specs/2026-10-07-hookcheck-design.md), [verification](docs/verification.md) and [contributing](CONTRIBUTING.md).

The module path is `github.com/ljn-forge/hookcheck`. Import the root package to execute native scenarios from Go, or install `cmd/hookcheck` for the CLI and external Hurl checks.

## License

MIT. See [LICENSE](LICENSE).
