package demo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Broken   bool
	Secret   string
	AckDelay time.Duration
}

func New(config Config) (http.Handler, error) {
	if config.Secret == "" {
		return nil, errors.New("demo requires a signing secret")
	}
	if config.AckDelay < 0 || config.AckDelay > time.Minute {
		return nil, errors.New("ack delay must be between 0 and 1m")
	}
	h := &handler{config: config, orders: map[string]state{}, seen: map[string][32]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /reset", h.reset)
	mux.HandleFunc("POST /webhooks", h.webhook)
	mux.HandleFunc("GET /state", h.state)
	return mux, nil
}

type state struct {
	Status  string `json:"status"`
	Version int    `json:"version"`
	Grants  int    `json:"grants"`
}

type event struct {
	ID      string `json:"id"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Version int    `json:"version"`
}

type handler struct {
	config Config
	mu     sync.Mutex
	orders map[string]state
	seen   map[string][32]byte
}

func (h *handler) readSigned(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "invalid or oversized body", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(h.config.Secret))
	mac.Write(body)
	signature, err := hex.DecodeString(r.Header.Get("X-Hookcheck-Signature"))
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return nil, false
	}
	return body, true
}

func (h *handler) reset(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.readSigned(w, r); !ok {
		return
	}
	h.mu.Lock()
	h.orders = map[string]state{}
	h.seen = map[string][32]byte{}
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readSigned(w, r)
	if !ok {
		return
	}
	var e event
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || d.Decode(new(any)) != io.EOF || e.ID == "" || e.OrderID == "" || e.Version <= 0 || e.Status != "paid" && e.Status != "pending" {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	if !h.apply(e, sha256.Sum256(body)) {
		http.Error(w, "event ID has conflicting content", http.StatusConflict)
		return
	}
	// State is committed before the ACK delay, demonstrating an uncertain send.
	if h.config.AckDelay > 0 {
		timer := time.NewTimer(h.config.AckDelay)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) apply(e event, digest [32]byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.config.Broken {
		if previous, exists := h.seen[e.ID]; exists {
			return previous == digest
		}
	}
	h.seen[e.ID] = digest
	s := h.orders[e.OrderID]
	if h.config.Broken {
		s.Status = e.Status
		s.Version = e.Version
		if e.Status == "paid" {
			s.Grants++
		}
	} else {
		// A paid fact is terminal, even if it arrives after a newer pending event.
		if e.Status == "paid" {
			s.Status = "paid"
			if s.Grants == 0 {
				s.Grants = 1
			}
		} else if s.Status != "paid" && e.Version > s.Version {
			s.Status = "pending"
		}
		s.Version = max(s.Version, e.Version)
	}
	h.orders[e.OrderID] = s
	return true
}

func (h *handler) state(w http.ResponseWriter, r *http.Request) {
	order := r.URL.Query().Get("order_id")
	if order == "" {
		order = "demo-order"
	}
	h.mu.Lock()
	snapshot := h.orders[order]
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshot)
}
