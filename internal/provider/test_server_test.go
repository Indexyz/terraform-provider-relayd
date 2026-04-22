// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

type requestRecord struct {
	Method string
	Path   string
	Body   string
}

type responseConfig struct {
	Status int
	Body   string
}

type relaydTestServer struct {
	t      *testing.T
	server *httptest.Server
	token  string

	mu          sync.Mutex
	nextID      int
	allocations map[string]relaydPortAllocation
	metrics     map[string]int64
	requests    []requestRecord

	nextCreateError       *responseConfig
	nextUpdateErrorByID   map[string]*responseConfig
	nextDeleteErrorByID   map[string]*responseConfig
	forceListUnauthorized bool
	forceMetricsError     *responseConfig
}

func newRelaydTestServer(t *testing.T) *relaydTestServer {
	t.Helper()

	ts := &relaydTestServer{
		t:                   t,
		token:               "test-token",
		nextID:              1,
		allocations:         map[string]relaydPortAllocation{},
		metrics:             map[string]int64{"allocations_total": 0, "tcp_active_sessions": 2},
		nextUpdateErrorByID: map[string]*responseConfig{},
		nextDeleteErrorByID: map[string]*responseConfig{},
	}

	ts.server = httptest.NewServer(http.HandlerFunc(ts.handle))
	return ts
}

func (ts *relaydTestServer) Close()        { ts.server.Close() }
func (ts *relaydTestServer) URL() string   { return ts.server.URL }
func (ts *relaydTestServer) Token() string { return ts.token }

func (ts *relaydTestServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+ts.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bodyBytes := []byte{}
	if r.Body != nil {
		bodyBytes, _ = ioReadAllAndClose(r.Body)
	}

	ts.mu.Lock()
	ts.requests = append(ts.requests, requestRecord{Method: r.Method, Path: r.URL.Path, Body: string(bodyBytes)})
	ts.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/ports":
		ts.handleCreate(w, bodyBytes)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/ports/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/ports/")
		if id == "target" {
			http.Error(w, "unexpected target endpoint", http.StatusTeapot)
			return
		}
		ts.handleUpdate(w, id, bodyBytes)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/ports/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/ports/")
		ts.handleDelete(w, id)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/ports":
		ts.handleList(w)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/metrics":
		ts.handleMetrics(w)
	default:
		http.NotFound(w, r)
	}
}

func (ts *relaydTestServer) handleCreate(w http.ResponseWriter, body []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.nextCreateError != nil {
		writePlainError(w, ts.nextCreateError.Status, ts.nextCreateError.Body)
		ts.nextCreateError = nil
		return
	}

	var req createPortAllocationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Protocol != "tcp" && req.Protocol != "udp" {
		writePlainError(w, http.StatusBadRequest, "invalid protocol")
		return
	}

	if req.TargetPort <= 0 {
		writePlainError(w, http.StatusBadRequest, "invalid target_port")
		return
	}

	id := fmt.Sprintf("alloc-%d", ts.nextID)
	listenPort := int64(10000 + ts.nextID)
	ts.nextID++
	allocation := relaydPortAllocation{
		ID:             id,
		Protocol:       req.Protocol,
		Port:           listenPort,
		TargetPort:     req.TargetPort,
		HostConfigured: false,
		RuntimeStatus:  "rejecting_no_host",
		CreatedAtMs:    int64(1712822400000 + ts.nextID),
		UpdatedAtMs:    int64(1712822405000 + ts.nextID),
	}
	ts.allocations[id] = allocation
	ts.metrics["allocations_total"] = int64(len(ts.allocations))
	writeJSON(w, http.StatusCreated, allocation)
}

func (ts *relaydTestServer) handleUpdate(w http.ResponseWriter, id string, body []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if cfg, ok := ts.nextUpdateErrorByID[id]; ok {
		delete(ts.nextUpdateErrorByID, id)
		writePlainError(w, cfg.Status, cfg.Body)
		return
	}

	allocation, ok := ts.allocations[id]
	if !ok {
		writePlainError(w, http.StatusNotFound, "not found")
		return
	}

	var req updatePortAllocationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.TargetPort == nil && req.Host == nil {
		writePlainError(w, http.StatusBadRequest, "empty update")
		return
	}

	if req.TargetPort != nil {
		allocation.TargetPort = *req.TargetPort
		effective := *req.TargetPort
		allocation.EffectiveTargetPort = &effective
	}

	if req.Host != nil {
		host := strings.TrimSpace(*req.Host)
		if host == "" || strings.Contains(host, "invalid") {
			writePlainError(w, http.StatusBadRequest, "InvalidHost")
			return
		}
		allocation.Host = &host
		allocation.EffectiveHost = &host
		allocation.HostConfigured = true
		allocation.RuntimeStatus = "active"
		if allocation.EffectiveTargetPort == nil {
			effective := allocation.TargetPort
			allocation.EffectiveTargetPort = &effective
		}
	}

	allocation.UpdatedAtMs++
	ts.allocations[id] = allocation
	writeJSON(w, http.StatusOK, allocation)
}

func (ts *relaydTestServer) handleDelete(w http.ResponseWriter, id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if cfg, ok := ts.nextDeleteErrorByID[id]; ok {
		delete(ts.nextDeleteErrorByID, id)
		writePlainError(w, cfg.Status, cfg.Body)
		return
	}

	if _, ok := ts.allocations[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
		return
	}

	delete(ts.allocations, id)
	ts.metrics["allocations_total"] = int64(len(ts.allocations))
	w.WriteHeader(http.StatusNoContent)
}

func (ts *relaydTestServer) handleList(w http.ResponseWriter) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.forceListUnauthorized {
		writePlainError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	allocations := make([]relaydPortAllocation, 0, len(ts.allocations))
	for _, allocation := range ts.allocations {
		allocations = append(allocations, allocation)
	}
	sort.SliceStable(allocations, func(i, j int) bool {
		if allocations[i].Protocol != allocations[j].Protocol {
			return allocations[i].Protocol < allocations[j].Protocol
		}
		if allocations[i].Port != allocations[j].Port {
			return allocations[i].Port < allocations[j].Port
		}
		return allocations[i].ID < allocations[j].ID
	})

	writeJSON(w, http.StatusOK, allocations)
}

func (ts *relaydTestServer) handleMetrics(w http.ResponseWriter) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.forceMetricsError != nil {
		cfg := ts.forceMetricsError
		ts.forceMetricsError = nil
		writePlainError(w, cfg.Status, cfg.Body)
		return
	}

	writeJSON(w, http.StatusOK, ts.metrics)
}

func (ts *relaydTestServer) requestPaths() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	paths := make([]string, 0, len(ts.requests))
	for _, req := range ts.requests {
		paths = append(paths, req.Path)
	}
	return paths
}

func (ts *relaydTestServer) removeAllocation(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.allocations, id)
}

func (ts *relaydTestServer) setNextUpdateError(id string, status int, body string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.nextUpdateErrorByID[id] = &responseConfig{Status: status, Body: body}
}

func (ts *relaydTestServer) setNextDeleteError(id string, status int, body string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.nextDeleteErrorByID[id] = &responseConfig{Status: status, Body: body}
}

func (ts *relaydTestServer) firstAllocationID() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ids := make([]string, 0, len(ts.allocations))
	for id := range ts.allocations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writePlainError(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func ioReadAllAndClose(rc io.ReadCloser) ([]byte, error) {
	defer rc.Close()
	return io.ReadAll(rc)
}

func testAccPortAllocationConfig(baseURL, token, protocol string, targetPort int, host *string) string {
	hostLine := ""
	if host != nil {
		hostLine = fmt.Sprintf("  host        = %q\n", *host)
	}

	return testAccProviderConfig(baseURL, token) + fmt.Sprintf(`
resource "relayd_port_allocation" "test" {
  protocol    = %q
  target_port = %d
%s}
`, protocol, targetPort, hostLine)
}

func testAccPortAllocationsDataSourceConfig(baseURL, token string) string {
	return testAccProviderConfig(baseURL, token) + `

data "relayd_port_allocations" "test" {}
`
}

func testAccMetricsDataSourceConfig(baseURL, token string) string {
	return testAccProviderConfig(baseURL, token) + `

data "relayd_metrics" "test" {}
`
}

func (ts *relaydTestServer) allocationCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.allocations)
}

func (ts *relaydTestServer) requestCount(method, path string) int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	count := 0
	for _, req := range ts.requests {
		if req.Method == method && req.Path == path {
			count++
		}
	}
	return count
}

func (ts *relaydTestServer) requestCountPrefix(method, prefix string) int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	count := 0
	for _, req := range ts.requests {
		if req.Method == method && strings.HasPrefix(req.Path, prefix) {
			count++
		}
	}
	return count
}
