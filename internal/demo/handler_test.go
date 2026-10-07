package demo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func post(t *testing.T, client *http.Client, base, path, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("demo-secret"))
	mac.Write([]byte(body))
	req.Header.Set("X-Hookcheck-Signature", hex.EncodeToString(mac.Sum(nil)))
	res, err := client.Do(req)
	if err != nil {
		t.Error(err)
		return 0
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func TestDemoBusinessIdempotencyAndOutOfOrderState(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "fixed", true: "broken"}[broken], func(t *testing.T) {
			h, err := New(Config{Broken: broken, Secret: "demo-secret"})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(h)
			defer server.Close()
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if got := post(t, server.Client(), server.URL, "/webhooks", `{"id":"paid-1","order_id":"demo-order","status":"paid","version":2}`); got != 204 {
						t.Errorf("HTTP=%d", got)
					}
				}()
			}
			wg.Wait()
			// A different event ID for the same order must not grant twice.
			post(t, server.Client(), server.URL, "/webhooks", `{"id":"paid-2","order_id":"demo-order","status":"paid","version":3}`)
			post(t, server.Client(), server.URL, "/webhooks", `{"id":"old","order_id":"demo-order","status":"pending","version":1}`)
			res, err := server.Client().Get(server.URL + "/state")
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var state struct {
				Grants  int    `json:"grants"`
				Status  string `json:"status"`
				Version int    `json:"version"`
			}
			if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
				t.Fatal(err)
			}
			if broken {
				if state.Grants != 13 || state.Status != "pending" {
					t.Fatalf("broken=%+v", state)
				}
			} else {
				if state.Grants != 1 || state.Status != "paid" || state.Version != 3 {
					t.Fatalf("fixed=%+v", state)
				}
			}
		})
	}
}

func TestDemoRejectsBadSignaturesAndConflictingEventIDs(t *testing.T) {
	h, err := New(Config{Secret: "demo-secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	res, err := server.Client().Post(server.URL+"/webhooks", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	post(t, server.Client(), server.URL, "/webhooks", `{"id":"one","order_id":"demo-order","status":"paid","version":2}`)
	if status := post(t, server.Client(), server.URL, "/webhooks", `{"id":"one","order_id":"demo-order","status":"pending","version":1}`); status != 409 {
		t.Fatalf("conflict=%d", status)
	}
	if status := post(t, server.Client(), server.URL, "/reset", `{}`); status != 204 {
		t.Fatalf("reset=%d", status)
	}
	if status := post(t, server.Client(), server.URL, "/webhooks", `{"id":"one","order_id":"demo-order","status":"paid","version":2}`); status != 204 {
		t.Fatalf("after reset=%d", status)
	}
}

func TestDemoPaidIsTerminalRegardlessOfArrivalOrder(t *testing.T) {
	paid := `{"id":"paid","order_id":"demo-order","status":"paid","version":2}`
	pending := `{"id":"pending","order_id":"demo-order","status":"pending","version":3}`
	for _, events := range [][]string{{paid, pending}, {pending, paid}} {
		h, err := New(Config{Secret: "demo-secret"})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(h)
		for _, e := range events {
			post(t, server.Client(), server.URL, "/webhooks", e)
		}
		res, err := server.Client().Get(server.URL + "/state")
		if err != nil {
			t.Fatal(err)
		}
		var s map[string]any
		json.NewDecoder(res.Body).Decode(&s)
		res.Body.Close()
		server.Close()
		if s["status"] != "paid" || s["grants"] != float64(1) || s["version"] != float64(3) {
			t.Fatalf("events=%v state=%v", events, s)
		}
	}
}
