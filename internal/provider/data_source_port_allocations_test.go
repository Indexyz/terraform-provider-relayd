// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccPortAllocationsDataSource(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	client, err := newRelaydClient(resolvedProviderConfig{BaseURL: ts.URL() + "/v1", BearerToken: ts.Token()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.CreateAllocation(t.Context(), createPortAllocationRequest{Protocol: "udp", TargetPort: 5353})
	if err != nil {
		t.Fatalf("seed allocation: %v", err)
	}
	_, err = client.CreateAllocation(t.Context(), createPortAllocationRequest{Protocol: "tcp", TargetPort: 8080})
	if err != nil {
		t.Fatalf("seed allocation: %v", err)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccPortAllocationsDataSourceConfig(ts.URL(), ts.Token()),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.relayd_port_allocations.test", tfjsonpath.New("allocations"), knownvalue.ListSizeExact(2)),
				statecheck.ExpectKnownValue("data.relayd_port_allocations.test", tfjsonpath.New("allocations").AtSliceIndex(0).AtMapKey("protocol"), knownvalue.StringExact("tcp")),
				statecheck.ExpectKnownValue("data.relayd_port_allocations.test", tfjsonpath.New("allocations").AtSliceIndex(0).AtMapKey("target_port"), knownvalue.Int64Exact(8080)),
				statecheck.ExpectKnownValue("data.relayd_port_allocations.test", tfjsonpath.New("allocations").AtSliceIndex(1).AtMapKey("protocol"), knownvalue.StringExact("udp")),
				statecheck.ExpectKnownValue("data.relayd_port_allocations.test", tfjsonpath.New("allocations").AtSliceIndex(1).AtMapKey("target_port"), knownvalue.Int64Exact(5353)),
			},
		}},
	})
}
