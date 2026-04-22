// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccPortAllocationResource_basicLifecycle(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	host := "127.0.0.1"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "tcp", 8080, &host),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("protocol"), knownvalue.StringExact("tcp")),
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("target_port"), knownvalue.Int64Exact(8080)),
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("host"), knownvalue.StringExact("127.0.0.1")),
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("runtime_status"), knownvalue.StringExact("active")),
				},
			},
			{
				ResourceName:      "relayd_port_allocation.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "tcp", 9090, &host),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("target_port"), knownvalue.Int64Exact(9090)),
				},
			},
		},
	})

	for _, path := range ts.requestPaths() {
		if path == "/v1/ports/target" {
			t.Fatal("provider unexpectedly used /v1/ports/target")
		}
	}
}

func TestAccPortAllocationResource_replacementScenarios(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	host := "127.0.0.1"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "tcp", 8080, &host)},
			{
				Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "udp", 8080, &host),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("protocol"), knownvalue.StringExact("udp")),
				},
			},
			{
				Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "udp", 8080, nil),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("runtime_status"), knownvalue.StringExact("rejecting_no_host")),
				},
			},
		},
	})

	if got := ts.requestCount(http.MethodPost, "/v1/ports"); got < 3 {
		t.Fatalf("expected at least three create requests across replacement scenarios, got %d", got)
	}
	if got := ts.requestCountPrefix(http.MethodDelete, "/v1/ports/"); got < 2 {
		t.Fatalf("expected replacement scenarios to issue at least two delete requests, got %d", got)
	}
}

func TestAccPortAllocationResource_createWithHostRollback(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					ts.setNextUpdateError("alloc-1", 503, "RuntimeUpdateFailed")
				},
				Config:      testAccPortAllocationConfig(ts.URL(), ts.Token(), "tcp", 8080, stringPtr("127.0.0.1")),
				ExpectError: regexp.MustCompile(`RuntimeUpdateFailed`),
			},
			{
				Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "tcp", 8080, nil),
			},
		},
	})

	if got := ts.requestCount(http.MethodDelete, "/v1/ports/alloc-1"); got != 1 {
		t.Fatalf("expected exactly one rollback delete for alloc-1, got %d", got)
	}
}

func TestAccPortAllocationResource_missingRefreshRemovesState(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccPortAllocationConfig(ts.URL(), ts.Token(), "tcp", 8080, nil)},
			{
				PreConfig: func() {
					id := ts.firstAllocationID()
					ts.removeAllocation(id)
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func stringPtr(v string) *string { return &v }
