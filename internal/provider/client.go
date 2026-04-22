// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	envBaseURL     = "RELAYD_BASE_URL"
	envBearerToken = "RELAYD_BEARER_TOKEN"
)

var errEmptyUpdate = errors.New("empty update")

type relaydClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	bearerToken string
}

type relaydAPIError struct {
	StatusCode int
	Message    string
}

func (e *relaydAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("unexpected API status: %d", e.StatusCode)
	}

	return fmt.Sprintf("%s (status %d)", e.Message, e.StatusCode)
}

type relaydPortAllocation struct {
	ID                  string  `json:"id"`
	Protocol            string  `json:"protocol"`
	Port                int64   `json:"port"`
	TargetPort          int64   `json:"target_port"`
	Host                *string `json:"host"`
	EffectiveTargetPort *int64  `json:"effective_target_port"`
	EffectiveHost       *string `json:"effective_host"`
	HostConfigured      bool    `json:"host_configured"`
	RuntimeStatus       string  `json:"runtime_status"`
	ErrorKind           *string `json:"error_kind"`
	LastError           *string `json:"last_error"`
	CreatedAtMs         int64   `json:"created_at_ms"`
	UpdatedAtMs         int64   `json:"updated_at_ms"`
}

type createPortAllocationRequest struct {
	Protocol   string `json:"protocol"`
	TargetPort int64  `json:"target_port"`
}

type updatePortAllocationRequest struct {
	TargetPort *int64  `json:"target_port,omitempty"`
	Host       *string `json:"host,omitempty"`
}

type resolvedProviderConfig struct {
	BaseURL     string
	BearerToken string
}

var allocationObjectTypes = map[string]attr.Type{
	"id":                    types.StringType,
	"protocol":              types.StringType,
	"port":                  types.Int64Type,
	"target_port":           types.Int64Type,
	"host":                  types.StringType,
	"effective_target_port": types.Int64Type,
	"effective_host":        types.StringType,
	"host_configured":       types.BoolType,
	"runtime_status":        types.StringType,
	"error_kind":            types.StringType,
	"last_error":            types.StringType,
	"created_at_ms":         types.Int64Type,
	"updated_at_ms":         types.Int64Type,
}

func resolveProviderConfig(baseURL, bearerToken types.String) (resolvedProviderConfig, error) {
	resolvedBaseURL := strings.TrimSpace(os.Getenv(envBaseURL))
	if !baseURL.IsNull() && !baseURL.IsUnknown() {
		resolvedBaseURL = strings.TrimSpace(baseURL.ValueString())
	}

	if baseURL.IsUnknown() {
		return resolvedProviderConfig{}, fmt.Errorf("base_url cannot be unknown")
	}

	resolvedToken := strings.TrimSpace(os.Getenv(envBearerToken))
	if !bearerToken.IsNull() && !bearerToken.IsUnknown() {
		resolvedToken = strings.TrimSpace(bearerToken.ValueString())
	}

	if bearerToken.IsUnknown() {
		return resolvedProviderConfig{}, fmt.Errorf("bearer_token cannot be unknown")
	}

	if resolvedBaseURL == "" {
		return resolvedProviderConfig{}, fmt.Errorf("base_url must be configured, either directly or via %s", envBaseURL)
	}

	normalizedBaseURL, err := normalizeBaseURL(resolvedBaseURL)
	if err != nil {
		return resolvedProviderConfig{}, err
	}

	if resolvedToken == "" {
		return resolvedProviderConfig{}, fmt.Errorf("bearer_token must be configured, either directly or via %s", envBearerToken)
	}

	return resolvedProviderConfig{
		BaseURL:     normalizedBaseURL,
		BearerToken: resolvedToken,
	}, nil
}

func normalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base_url must include scheme and host")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if path == "" || path == "/" {
		path = "/v1"
	} else if !strings.HasSuffix(path, "/v1") {
		path = path + "/v1"
	}

	parsed.Path = path
	parsed.RawPath = ""
	parsed.ForceQuery = false
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

func newRelaydClient(cfg resolvedProviderConfig) (*relaydClient, error) {
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}

	return &relaydClient{
		baseURL:     parsed,
		httpClient:  http.DefaultClient,
		bearerToken: cfg.BearerToken,
	}, nil
}

func (c *relaydClient) CreateAllocation(ctx context.Context, req createPortAllocationRequest) (relaydPortAllocation, error) {
	var allocation relaydPortAllocation
	if err := c.doJSON(ctx, http.MethodPost, "/ports", req, http.StatusCreated, &allocation); err != nil {
		return relaydPortAllocation{}, err
	}

	return allocation, nil
}

func (c *relaydClient) UpdateAllocation(ctx context.Context, id string, req updatePortAllocationRequest) (relaydPortAllocation, error) {
	if req.TargetPort == nil && req.Host == nil {
		return relaydPortAllocation{}, errEmptyUpdate
	}

	var allocation relaydPortAllocation
	if err := c.doJSON(ctx, http.MethodPost, "/ports/"+id, req, http.StatusOK, &allocation); err != nil {
		return relaydPortAllocation{}, err
	}

	return allocation, nil
}

func (c *relaydClient) DeleteAllocation(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/ports/"+id, nil, http.StatusNoContent, nil)
}

func (c *relaydClient) ListAllocations(ctx context.Context) ([]relaydPortAllocation, error) {
	var allocations []relaydPortAllocation
	if err := c.doJSON(ctx, http.MethodGet, "/ports", nil, http.StatusOK, &allocations); err != nil {
		return nil, err
	}

	sortPortAllocations(allocations)
	return allocations, nil
}

func (c *relaydClient) GetMetrics(ctx context.Context) (map[string]int64, error) {
	var metrics map[string]int64
	if err := c.doJSON(ctx, http.MethodGet, "/metrics", nil, http.StatusOK, &metrics); err != nil {
		return nil, err
	}

	if metrics == nil {
		metrics = map[string]int64{}
	}

	return metrics, nil
}

func (c *relaydClient) doJSON(ctx context.Context, method, endpoint string, requestBody any, expectedStatus int, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.resolveURL(endpoint), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound && method == http.MethodDelete {
		return nil
	}

	if resp.StatusCode != expectedStatus {
		message := strings.TrimSpace(string(payload))
		if message == "" {
			message = resp.Status
		}
		return &relaydAPIError{StatusCode: resp.StatusCode, Message: message}
	}

	if responseBody == nil || len(payload) == 0 {
		return nil
	}

	if err := json.Unmarshal(payload, responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func (c *relaydClient) resolveURL(endpoint string) string {
	resolved := *c.baseURL
	resolved.Path = strings.TrimRight(c.baseURL.Path, "/") + endpoint
	return resolved.String()
}

func findAllocationByID(allocations []relaydPortAllocation, id string) (relaydPortAllocation, bool) {
	for _, allocation := range allocations {
		if allocation.ID == id {
			return allocation, true
		}
	}

	return relaydPortAllocation{}, false
}

func sortPortAllocations(allocations []relaydPortAllocation) {
	sort.SliceStable(allocations, func(i, j int) bool {
		if allocations[i].Protocol != allocations[j].Protocol {
			return allocations[i].Protocol < allocations[j].Protocol
		}
		if allocations[i].Port != allocations[j].Port {
			return allocations[i].Port < allocations[j].Port
		}
		return allocations[i].ID < allocations[j].ID
	})
}

func stringValueOrNull(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}

	return types.StringValue(*value)
}

func int64ValueOrNull(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}

	return types.Int64Value(*value)
}

func allocationToObjectValue(_ context.Context, allocation relaydPortAllocation) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(allocationObjectTypes, map[string]attr.Value{
		"id":                    types.StringValue(allocation.ID),
		"protocol":              types.StringValue(allocation.Protocol),
		"port":                  types.Int64Value(allocation.Port),
		"target_port":           types.Int64Value(allocation.TargetPort),
		"host":                  stringValueOrNull(allocation.Host),
		"effective_target_port": int64ValueOrNull(allocation.EffectiveTargetPort),
		"effective_host":        stringValueOrNull(allocation.EffectiveHost),
		"host_configured":       types.BoolValue(allocation.HostConfigured),
		"runtime_status":        types.StringValue(allocation.RuntimeStatus),
		"error_kind":            stringValueOrNull(allocation.ErrorKind),
		"last_error":            stringValueOrNull(allocation.LastError),
		"created_at_ms":         types.Int64Value(allocation.CreatedAtMs),
		"updated_at_ms":         types.Int64Value(allocation.UpdatedAtMs),
	})
}

func addError(diags *diag.Diagnostics, summary string, err error) {
	if err == nil {
		return
	}

	diags.AddError(summary, err.Error())
}
