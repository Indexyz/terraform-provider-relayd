// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccPortBindingResource_basicLifecycle(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBindingConfig(ts.URL(), ts.Token(), "tcp", "127.0.0.1", 8080),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_binding.test", tfjsonpath.New("host"), knownvalue.StringExact("127.0.0.1")),
					statecheck.ExpectKnownValue("relayd_port_binding.test", tfjsonpath.New("target_port"), knownvalue.Int64Exact(8080)),
					statecheck.ExpectKnownValue("relayd_port_binding.test", tfjsonpath.New("runtime_status"), knownvalue.StringExact("active")),
				},
			},
			{
				ResourceName:      "relayd_port_binding.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccBindingConfig(ts.URL(), ts.Token(), "tcp", "127.0.0.2", 9090),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_binding.test", tfjsonpath.New("host"), knownvalue.StringExact("127.0.0.2")),
					statecheck.ExpectKnownValue("relayd_port_binding.test", tfjsonpath.New("target_port"), knownvalue.Int64Exact(9090)),
				},
			},
		},
	})
}

func TestAccPortBindingResource_deleteKeepsAllocation(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccBindingConfig(ts.URL(), ts.Token(), "tcp", "127.0.0.1", 8080)},
			{
				Config: testAccAllocationAndBindingDetachedConfig(ts.URL(), ts.Token(), "tcp"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("relayd_port_allocation.alloc", tfjsonpath.New("protocol"), knownvalue.StringExact("tcp")),
				},
			},
		},
	})
}

func TestAccPortBindingResource_invalidHost(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      testAccBindingConfig(ts.URL(), ts.Token(), "tcp", "invalid-host", 8080),
			ExpectError: regexp.MustCompile(`InvalidHost|invalid host`),
		}},
	})
}

func TestAccPortBindingResource_missingRefreshRemovesState(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccBindingConfig(ts.URL(), ts.Token(), "tcp", "127.0.0.1", 8080)},
			{
				PreConfig: func() {
					id := ts.firstAllocationID()
					ts.removeBinding(id)
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
