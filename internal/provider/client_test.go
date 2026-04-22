// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestResolveProviderConfig_EnvironmentFallback(t *testing.T) {
	t.Setenv(envBaseURL, "http://127.0.0.1:8080")
	t.Setenv(envBearerToken, "env-token")

	resolved, err := resolveProviderConfig(types.StringNull(), types.StringNull())
	if err != nil {
		t.Fatalf("expected config to resolve from env, got error: %v", err)
	}

	if resolved.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("unexpected resolved base URL: %s", resolved.BaseURL)
	}
	if resolved.BearerToken != "env-token" {
		t.Fatalf("unexpected resolved token: %s", resolved.BearerToken)
	}
}

func TestResolveProviderConfig_ExplicitConfigOverridesEnvironment(t *testing.T) {
	t.Setenv(envBaseURL, "http://127.0.0.1:8080")
	t.Setenv(envBearerToken, "env-token")

	resolved, err := resolveProviderConfig(types.StringValue("http://127.0.0.1:9090/api"), types.StringValue("config-token"))
	if err != nil {
		t.Fatalf("expected explicit config to resolve, got error: %v", err)
	}

	if resolved.BaseURL != "http://127.0.0.1:9090/api/v1" {
		t.Fatalf("unexpected resolved base URL: %s", resolved.BaseURL)
	}
	if resolved.BearerToken != "config-token" {
		t.Fatalf("unexpected resolved token: %s", resolved.BearerToken)
	}
}

func TestResolveProviderConfig_MissingValues(t *testing.T) {
	os.Unsetenv(envBaseURL)
	os.Unsetenv(envBearerToken)

	_, err := resolveProviderConfig(types.StringNull(), types.StringNull())
	if err == nil {
		t.Fatal("expected error when configuration is missing")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := map[string]string{
		"http://127.0.0.1:8080":        "http://127.0.0.1:8080/v1",
		"http://127.0.0.1:8080/":       "http://127.0.0.1:8080/v1",
		"http://127.0.0.1:8080/v1":     "http://127.0.0.1:8080/v1",
		"http://127.0.0.1:8080/proxy":  "http://127.0.0.1:8080/proxy/v1",
		"http://127.0.0.1:8080/proxy/": "http://127.0.0.1:8080/proxy/v1",
	}

	for raw, expected := range tests {
		t.Run(raw, func(t *testing.T) {
			got, err := normalizeBaseURL(raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != expected {
				t.Fatalf("expected %s, got %s", expected, got)
			}
		})
	}
}

func TestSortPortAllocations(t *testing.T) {
	allocations := []relaydPortAllocation{
		{ID: "2", Protocol: "udp", Port: 2000},
		{ID: "3", Protocol: "tcp", Port: 3000},
		{ID: "1", Protocol: "tcp", Port: 1000},
		{ID: "4", Protocol: "tcp", Port: 1000},
	}

	sortPortAllocations(allocations)

	gotIDs := []string{allocations[0].ID, allocations[1].ID, allocations[2].ID, allocations[3].ID}
	expectedIDs := []string{"1", "4", "3", "2"}
	for i := range expectedIDs {
		if gotIDs[i] != expectedIDs[i] {
			t.Fatalf("unexpected ordering: got %v want %v", gotIDs, expectedIDs)
		}
	}
}

func TestRelaydClient_Delete404IsSuccess(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	client, err := newRelaydClient(resolvedProviderConfig{BaseURL: ts.URL() + "/v1", BearerToken: ts.Token()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := client.DeleteAllocation(t.Context(), "missing-id"); err != nil {
		t.Fatalf("expected delete 404 to be treated as success, got: %v", err)
	}
}

func TestRelaydClient_MapsPlainTextErrors(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()
	ts.forceMetricsError = &responseConfig{Status: 503, Body: "RuntimeUpdateFailed"}

	client, err := newRelaydClient(resolvedProviderConfig{BaseURL: ts.URL() + "/v1", BearerToken: ts.Token()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetMetrics(t.Context())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "RuntimeUpdateFailed") {
		t.Fatalf("expected plain text error to be preserved, got: %v", err)
	}
}
