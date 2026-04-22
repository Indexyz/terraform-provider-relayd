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

func TestAccMetricsDataSource(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccMetricsDataSourceConfig(ts.URL(), ts.Token()),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.relayd_metrics.test", tfjsonpath.New("metrics"), knownvalue.MapSizeExact(2)),
				statecheck.ExpectKnownValue("data.relayd_metrics.test", tfjsonpath.New("metrics").AtMapKey("allocations_total"), knownvalue.Int64Exact(0)),
			},
		}},
	})
}

func TestAccMetricsDataSource_unauthorized(t *testing.T) {
	ts := newRelaydTestServer(t)
	defer ts.Close()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      testAccMetricsDataSourceConfig(ts.URL(), "wrong-token"),
			ExpectError: regexp.MustCompile(`unauthorized`),
		}},
	})
}
