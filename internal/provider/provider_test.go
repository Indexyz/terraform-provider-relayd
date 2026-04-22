// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"relayd": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
}

func testAccProviderConfig(baseURL, bearerToken string) string {
	return fmt.Sprintf(`
provider "relayd" {
  base_url     = %[1]q
  bearer_token = %[2]q
}
`, baseURL, bearerToken)
}
