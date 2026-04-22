// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccPortAllocationResource_basicLifecycle(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccAllocationConfig(ts.URL(), ts.Token(), "tcp"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("protocol"), knownvalue.StringExact("tcp")),
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("port"), knownvalue.Int64Exact(10001)),
				},
			},
			{
				ResourceName:      "relayd_port_allocation.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccPortAllocationResource_replaceOnProtocolChange(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccAllocationConfig(ts.URL(), ts.Token(), "tcp")},
			{
				Config: testAccAllocationConfig(ts.URL(), ts.Token(), "udp"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.test", tfjsonpath.New("protocol"), knownvalue.StringExact("udp")),
				},
			},
		},
	})

	if got := ts.requestCount(http.MethodPost, "/v1/allocations"); got < 2 {
		t.Fatalf("expected replacement to create a new allocation, got %d create requests", got)
	}
	if got := ts.requestCountPrefix(http.MethodDelete, "/v1/allocations/"); got < 1 {
		t.Fatalf("expected replacement to delete the old allocation, got %d delete requests", got)
	}
}

func TestAccPortAllocationResource_missingRefreshRemovesState(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccAllocationConfig(ts.URL(), ts.Token(), "tcp")},
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
