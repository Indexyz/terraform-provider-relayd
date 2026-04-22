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

type relaydAllocation struct {
	ID          string `json:"id"`
	Protocol    string `json:"protocol"`
	Port        int64  `json:"port"`
	CreatedAtMs int64  `json:"created_at_ms"`
	UpdatedAtMs int64  `json:"updated_at_ms"`
}

type relaydBinding struct {
	AllocationID        string  `json:"allocation_id"`
	Host                string  `json:"host"`
	TargetPort          int64   `json:"target_port"`
	EffectiveTargetPort *int64  `json:"effective_target_port"`
	EffectiveHost       *string `json:"effective_host"`
	RuntimeStatus       string  `json:"runtime_status"`
	ErrorKind           *string `json:"error_kind"`
	LastError           *string `json:"last_error"`
	CreatedAtMs         int64   `json:"created_at_ms"`
	UpdatedAtMs         int64   `json:"updated_at_ms"`
}

type createAllocationRequest struct {
	Protocol string `json:"protocol"`
}

type putBindingRequest struct {
	Host       string `json:"host"`
	TargetPort int64  `json:"target_port"`
}

type resolvedProviderConfig struct {
	BaseURL     string
	BearerToken string
}

var allocationObjectTypes = map[string]attr.Type{
	"id":            types.StringType,
	"protocol":      types.StringType,
	"port":          types.Int64Type,
	"created_at_ms": types.Int64Type,
	"updated_at_ms": types.Int64Type,
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
	if resolvedToken == "" {
		return resolvedProviderConfig{}, fmt.Errorf("bearer_token must be configured, either directly or via %s", envBearerToken)
	}

	normalizedBaseURL, err := normalizeBaseURL(resolvedBaseURL)
	if err != nil {
		return resolvedProviderConfig{}, err
	}

	return resolvedProviderConfig{BaseURL: normalizedBaseURL, BearerToken: resolvedToken}, nil
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
		path += "/v1"
	}

	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.ForceQuery = false
	return parsed.String(), nil
}

func newRelaydClient(cfg resolvedProviderConfig) (*relaydClient, error) {
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	return &relaydClient{baseURL: parsed, httpClient: http.DefaultClient, bearerToken: cfg.BearerToken}, nil
}

func (c *relaydClient) CreateAllocation(ctx context.Context, req createAllocationRequest) (relaydAllocation, error) {
	var allocation relaydAllocation
	if err := c.doJSON(ctx, http.MethodPost, "/allocations", req, http.StatusCreated, &allocation); err != nil {
		return relaydAllocation{}, err
	}
	return allocation, nil
}

func (c *relaydClient) ListAllocations(ctx context.Context) ([]relaydAllocation, error) {
	var allocations []relaydAllocation
	if err := c.doJSON(ctx, http.MethodGet, "/allocations", nil, http.StatusOK, &allocations); err != nil {
		return nil, err
	}
	sortAllocations(allocations)
	return allocations, nil
}

func (c *relaydClient) GetAllocation(ctx context.Context, id string) (relaydAllocation, error) {
	var allocation relaydAllocation
	if err := c.doJSON(ctx, http.MethodGet, "/allocations/"+id, nil, http.StatusOK, &allocation); err != nil {
		return relaydAllocation{}, err
	}
	return allocation, nil
}

func (c *relaydClient) DeleteAllocation(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/allocations/"+id, nil, http.StatusNoContent, nil)
}

func (c *relaydClient) PutBinding(ctx context.Context, allocationID string, req putBindingRequest) (relaydBinding, error) {
	var binding relaydBinding
	if err := c.doJSON(ctx, http.MethodPut, "/allocations/"+allocationID+"/binding", req, http.StatusOK, &binding); err != nil {
		return relaydBinding{}, err
	}
	return binding, nil
}

func (c *relaydClient) GetBinding(ctx context.Context, allocationID string) (relaydBinding, error) {
	var binding relaydBinding
	if err := c.doJSON(ctx, http.MethodGet, "/allocations/"+allocationID+"/binding", nil, http.StatusOK, &binding); err != nil {
		return relaydBinding{}, err
	}
	return binding, nil
}

func (c *relaydClient) DeleteBinding(ctx context.Context, allocationID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/allocations/"+allocationID+"/binding", nil, http.StatusNoContent, nil)
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

	if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
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

func sortAllocations(allocations []relaydAllocation) {
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

func allocationToObjectValue(allocation relaydAllocation) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(allocationObjectTypes, map[string]attr.Value{
		"id":            types.StringValue(allocation.ID),
		"protocol":      types.StringValue(allocation.Protocol),
		"port":          types.Int64Value(allocation.Port),
		"created_at_ms": types.Int64Value(allocation.CreatedAtMs),
		"updated_at_ms": types.Int64Value(allocation.UpdatedAtMs),
	})
}

func isNotFoundError(err error) bool {
	var apiErr *relaydAPIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func addError(diags *diag.Diagnostics, summary string, err error) {
	if err == nil {
		return
	}
	diags.AddError(summary, err.Error())
}
