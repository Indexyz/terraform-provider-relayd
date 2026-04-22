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
	allocations map[string]relaydAllocation
	bindings    map[string]relaydBinding
	metrics     map[string]int64
	requests    []requestRecord

	nextCreateAllocationError *responseConfig
	nextPutBindingErrorByID   map[string]*responseConfig
	nextDeleteAllocationError map[string]*responseConfig
	nextDeleteBindingError    map[string]*responseConfig
	forceListUnauthorized     bool
	forceMetricsError         *responseConfig
}

func newRelaydTestServer(t *testing.T) *relaydTestServer {
	t.Helper()

	ts := &relaydTestServer{
		t:                        t,
		token:                    "test-token",
		nextID:                   1,
		allocations:              map[string]relaydAllocation{},
		bindings:                 map[string]relaydBinding{},
		metrics:                  map[string]int64{"allocations_total": 0, "tcp_active_sessions": 2},
		nextPutBindingErrorByID:  map[string]*responseConfig{},
		nextDeleteAllocationError: map[string]*responseConfig{},
		nextDeleteBindingError:   map[string]*responseConfig{},
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
	case r.Method == http.MethodPost && r.URL.Path == "/v1/allocations":
		ts.handleCreateAllocation(w, bodyBytes)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/allocations":
		ts.handleListAllocations(w)
	case strings.HasPrefix(r.URL.Path, "/v1/allocations/"):
		path := strings.TrimPrefix(r.URL.Path, "/v1/allocations/")
		parts := strings.Split(path, "/")
		id := parts[0]
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				ts.handleGetAllocation(w, id)
			case http.MethodDelete:
				ts.handleDeleteAllocation(w, id)
			default:
				http.NotFound(w, r)
			}
			return
		}
		if len(parts) == 2 && parts[1] == "binding" {
			switch r.Method {
			case http.MethodGet:
				ts.handleGetBinding(w, id)
			case http.MethodPut:
				ts.handlePutBinding(w, id, bodyBytes)
			case http.MethodDelete:
				ts.handleDeleteBinding(w, id)
			default:
				http.NotFound(w, r)
			}
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/metrics":
		ts.handleMetrics(w)
	default:
		http.NotFound(w, r)
	}
}

func (ts *relaydTestServer) handleCreateAllocation(w http.ResponseWriter, body []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.nextCreateAllocationError != nil {
		writePlainError(w, ts.nextCreateAllocationError.Status, ts.nextCreateAllocationError.Body)
		ts.nextCreateAllocationError = nil
		return
	}

	var req createAllocationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Protocol != "tcp" && req.Protocol != "udp" {
		writePlainError(w, http.StatusBadRequest, "invalid protocol")
		return
	}

	id := fmt.Sprintf("alloc-%d", ts.nextID)
	port := int64(10000 + ts.nextID)
	ts.nextID++
	allocation := relaydAllocation{
		ID:          id,
		Protocol:    req.Protocol,
		Port:        port,
		CreatedAtMs: int64(1712822400000 + ts.nextID),
		UpdatedAtMs: int64(1712822400000 + ts.nextID),
	}
	ts.allocations[id] = allocation
	ts.metrics["allocations_total"] = int64(len(ts.allocations))
	writeJSON(w, http.StatusCreated, allocation)
}

func (ts *relaydTestServer) handleListAllocations(w http.ResponseWriter) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.forceListUnauthorized {
		writePlainError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	allocations := make([]relaydAllocation, 0, len(ts.allocations))
	for _, allocation := range ts.allocations {
		allocations = append(allocations, allocation)
	}
	sortAllocations(allocations)
	writeJSON(w, http.StatusOK, allocations)
}

func (ts *relaydTestServer) handleGetAllocation(w http.ResponseWriter, id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	allocation, ok := ts.allocations[id]
	if !ok {
		writePlainError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, allocation)
}

func (ts *relaydTestServer) handleDeleteAllocation(w http.ResponseWriter, id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if cfg, ok := ts.nextDeleteAllocationError[id]; ok {
		delete(ts.nextDeleteAllocationError, id)
		writePlainError(w, cfg.Status, cfg.Body)
		return
	}

	if _, ok := ts.allocations[id]; !ok {
		writePlainError(w, http.StatusNotFound, "not found")
		return
	}
	delete(ts.allocations, id)
	delete(ts.bindings, id)
	ts.metrics["allocations_total"] = int64(len(ts.allocations))
	w.WriteHeader(http.StatusNoContent)
}

func (ts *relaydTestServer) handlePutBinding(w http.ResponseWriter, id string, body []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if cfg, ok := ts.nextPutBindingErrorByID[id]; ok {
		delete(ts.nextPutBindingErrorByID, id)
		writePlainError(w, cfg.Status, cfg.Body)
		return
	}
	if _, ok := ts.allocations[id]; !ok {
		writePlainError(w, http.StatusNotFound, "not found")
		return
	}

	var req putBindingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Host) == "" || strings.Contains(req.Host, "invalid") {
		writePlainError(w, http.StatusBadRequest, "InvalidHost")
		return
	}
	if req.TargetPort <= 0 {
		writePlainError(w, http.StatusBadRequest, "invalid target_port")
		return
	}

	binding, exists := ts.bindings[id]
	if !exists {
		binding = relaydBinding{
			AllocationID: id,
			CreatedAtMs:  int64(1712822401000 + ts.nextID),
		}
	}
	host := strings.TrimSpace(req.Host)
	effectivePort := req.TargetPort
	effectiveHost := host
	binding.Host = host
	binding.TargetPort = req.TargetPort
	binding.EffectiveTargetPort = &effectivePort
	binding.EffectiveHost = &effectiveHost
	binding.RuntimeStatus = "active"
	binding.ErrorKind = nil
	binding.LastError = nil
	if binding.CreatedAtMs == 0 {
		binding.CreatedAtMs = int64(1712822401000 + ts.nextID)
	}
	binding.UpdatedAtMs = binding.CreatedAtMs + 1
	ts.bindings[id] = binding
	writeJSON(w, http.StatusOK, binding)
}

func (ts *relaydTestServer) handleGetBinding(w http.ResponseWriter, id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if _, ok := ts.allocations[id]; !ok {
		writePlainError(w, http.StatusNotFound, "not found")
		return
	}
	binding, ok := ts.bindings[id]
	if !ok {
		writePlainError(w, http.StatusNotFound, "binding not found")
		return
	}
	writeJSON(w, http.StatusOK, binding)
}

func (ts *relaydTestServer) handleDeleteBinding(w http.ResponseWriter, id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if cfg, ok := ts.nextDeleteBindingError[id]; ok {
		delete(ts.nextDeleteBindingError, id)
		writePlainError(w, cfg.Status, cfg.Body)
		return
	}
	if _, ok := ts.allocations[id]; !ok {
		writePlainError(w, http.StatusNotFound, "not found")
		return
	}
	if _, ok := ts.bindings[id]; !ok {
		writePlainError(w, http.StatusNotFound, "binding not found")
		return
	}
	delete(ts.bindings, id)
	w.WriteHeader(http.StatusNoContent)
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
	delete(ts.bindings, id)
}

func (ts *relaydTestServer) removeBinding(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.bindings, id)
}

func (ts *relaydTestServer) setNextPutBindingError(id string, status int, body string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.nextPutBindingErrorByID[id] = &responseConfig{Status: status, Body: body}
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

func (ts *relaydTestServer) allocationCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.allocations)
}

func (ts *relaydTestServer) bindingCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.bindings)
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

func testAccAllocationConfig(baseURL, token, protocol string) string {
	return testAccProviderConfig(baseURL, token) + fmt.Sprintf(`
resource "relayd_port_allocation" "test" {
  protocol = %q
}
`, protocol)
}

func testAccBindingConfig(baseURL, token, protocol string, host string, targetPort int) string {
	return testAccProviderConfig(baseURL, token) + fmt.Sprintf(`
resource "relayd_port_allocation" "alloc" {
  protocol = %q
}

resource "relayd_port_binding" "test" {
  allocation_id = relayd_port_allocation.alloc.id
  host          = %q
  target_port   = %d
}
`, protocol, host, targetPort)
}

func testAccAllocationAndBindingDetachedConfig(baseURL, token, protocol string) string {
	return testAccProviderConfig(baseURL, token) + fmt.Sprintf(`
resource "relayd_port_allocation" "alloc" {
  protocol = %q
}
`, protocol)
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
