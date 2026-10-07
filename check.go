package hookcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"time"
)

func runCheck(parent context.Context, client *http.Client, base string, headers map[string]string, check Check, timeout time.Duration) CheckResult {
	started := time.Now()
	result := CheckResult{Name: check.Name}
	ctx := parent
	within, _ := duration(check.Within, 0)
	if within > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, within)
		defer cancel()
	}
	interval, _ := duration(check.Interval, 50*time.Millisecond)
	for {
		if ctx.Err() != nil {
			if parent.Err() != nil {
				result.Outcome = "cancelled"
				result.Message = "check cancelled"
			} else if result.Outcome == "" {
				result.Outcome = "failed"
				result.Message = "check window expired"
			}
			break
		}
		r := request(ctx, client, base+check.Path, http.MethodGet, nil, headers, nil, nil, "", timeout, defaultInt(check.ExpectStatus, 200))
		result.Polls++
		result.Status = r.status
		result.Outcome = r.outcome
		result.Message = r.message
		if r.outcome == "passed" {
			if err := matchFields(r.body, check.Fields); err != nil {
				result.Outcome = "failed"
				result.Message = err.Error()
			}
		}
		if result.Outcome == "passed" || within == 0 {
			break
		}
		if ctx.Err() != nil && parent.Err() == nil {
			result.Outcome = "failed"
			result.Message = "check window expired before assertions passed"
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result
}

func matchFields(body []byte, fields map[string]json.RawMessage) error {
	if len(fields) == 0 {
		return nil
	}
	var actual map[string]json.RawMessage
	if err := decodeJSON(body, &actual); err != nil || actual == nil {
		return fmt.Errorf("check response must be one JSON object")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, ok := actual[key]
		if !ok {
			return fmt.Errorf("field %q is missing", key)
		}
		var got, want any
		if decodeJSON(value, &got) != nil || decodeJSON(fields[key], &want) != nil || !reflect.DeepEqual(got, want) {
			return fmt.Errorf("field %q did not match", key)
		}
	}
	return nil
}

func decodeJSON(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
