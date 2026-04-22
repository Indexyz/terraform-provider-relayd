# Terraform Provider relayd

Terraform provider for the `relayd` control plane. It configures access to the authenticated HTTP API, manages port allocations, and reads runtime metrics.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.24

## Example configuration

```terraform
provider "relayd" {
  base_url     = "http://127.0.0.1:8080"
  bearer_token = "replace-me"
}
```

`base_url` and `bearer_token` can also be supplied via `RELAYD_BASE_URL` and `RELAYD_BEARER_TOKEN`.

## Available surfaces

- `relayd_port_allocation` resource
- `relayd_port_allocations` data source
- `relayd_metrics` data source

## Developing

- Run `make generate` after updating the provider implementation so the generated docs and examples stay in sync.
- Run `make testacc` for acceptance tests.
