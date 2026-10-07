package hookcheck

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxResponseBytes = 1 << 20

type response struct {
	status  int
	outcome string
	message string
	body    []byte
}

func request(parent context.Context, client *http.Client, target, method string, body []byte, headers, extra map[string]string, signing *Signing, secret string, timeout time.Duration, expected int) response {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return response{outcome: "transport_error", message: "could not construct HTTP request"}
	}
	// NewRequest installs GetBody for bytes.Reader. Disable replay even when an
	// Idempotency-Key makes a POST eligible for transparent Transport retries.
	if method == http.MethodPost {
		req.GetBody = nil
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for key, value := range extra {
		req.Header.Set(key, value)
	}
	if signing != nil {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set(signing.Header, hex.EncodeToString(mac.Sum(nil)))
	}
	res, err := client.Do(req)
	if err != nil {
		return requestFailure(parent, ctx)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		result := requestFailure(parent, ctx)
		result.status = res.StatusCode
		return result
	}
	if len(data) > maxResponseBytes {
		return response{status: res.StatusCode, outcome: "response_too_large", message: "response exceeds 1 MiB"}
	}
	result := response{status: res.StatusCode, outcome: "passed", body: data}
	if expected != 0 && res.StatusCode != expected || expected == 0 && (res.StatusCode < 200 || res.StatusCode >= 300) {
		result.outcome = "failed"
		result.message = fmt.Sprintf("unexpected HTTP status %d", res.StatusCode)
	}
	return result
}

func requestFailure(parent, requestContext context.Context) response {
	if parent.Err() != nil {
		return response{outcome: "cancelled", message: "request cancelled by execution context"}
	}
	if errors.Is(requestContext.Err(), context.DeadlineExceeded) {
		return response{outcome: "timeout", message: "request timed out; server acceptance is unknown"}
	}
	return response{outcome: "transport_error", message: "HTTP transport or response read failed"}
}
