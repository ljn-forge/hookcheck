// Package hookcheck executes bounded webhook scenarios and verifies final
// business state. Load validates a versioned JSON scenario; Run performs only
// its explicit deliveries and checks. Failed test outcomes are returned in the
// report, while configuration errors and context cancellation are Go errors.
//
// A scenario must remain immutable during Run. Reports deliberately exclude
// payloads, headers and secret values. Run rejects scenarios with HurlFile;
// optional external Hurl checks are owned and executed by the CLI.
package hookcheck
