# Contributing

Start with a reproducible scenario, bug report, or focused improvement. Explain the observable problem and the intended behavior before expanding the scenario language.

## Local checks

Use Go 1.25+ on Linux or macOS:

```sh
make check
make fuzz
make smoke
```

For bugs, first add a failing test using real HTTP where possible. Prefer standard-library facilities and existing packages over new protocol/query implementations. Keep the execution core independent of CLI, filesystem and subprocess concerns. Add dependencies only with a documented compatibility, maintenance and license comparison.

## Contracts to preserve

- Each POST is one explicit planned delivery; no hidden retries or redirects.
- Cancellation stops dispatch and waits for owned workers and supported subprocesses.
- Passing delivery status cannot substitute for a business assertion.
- Scenario validation happens before network effects.
- Report payloads and process output must not expose bodies, headers or secrets.
- The same seed reproduces a plan, not concurrent arrival timing.

Do not add abstract provider interfaces without multiple actual implementations. Add provider signature formats through their maintained SDKs when appropriate. Demonstrate any behavior change in a small executable fixture.

Pull requests should explain the concrete problem, resulting behavior, tests run and limitations. Keep unrelated formatting and changes out of the patch. The fixture is an in-memory test service, not a production payment integration.
