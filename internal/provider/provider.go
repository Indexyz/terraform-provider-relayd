// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &RelaydProvider{}

type RelaydProvider struct {
	version string
}

type RelaydProviderModel struct {
	BaseURL     types.String `tfsdk:"base_url"`
	BearerToken types.String `tfsdk:"bearer_token"`
}

func (p *RelaydProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "relayd"
	resp.Version = p.version
}

func (p *RelaydProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		MarkdownDescription: "The relayd provider manages authenticated HTTP control-plane resources exposed by relayd.",
		Attributes: map[string]providerschema.Attribute{
			"base_url":     providerschema.StringAttribute{MarkdownDescription: "Base URL for the relayd API server. If omitted, the provider uses the RELAYD_BASE_URL environment variable. The `/v1` API path is added automatically when necessary.", Optional: true},
			"bearer_token": providerschema.StringAttribute{MarkdownDescription: "Bearer token for relayd API authentication. If omitted, the provider uses the RELAYD_BEARER_TOKEN environment variable.", Optional: true, Sensitive: true},
		},
	}
}

func (p *RelaydProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data RelaydProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resolvedConfig, err := resolveProviderConfig(data.BaseURL, data.BearerToken)
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}
	client, err := newRelaydClient(resolvedConfig)
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure relayd client", err.Error())
		return
	}
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *RelaydProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPortAllocationResource,
		NewPortBindingResource,
	}
}

func (p *RelaydProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewPortAllocationsDataSource,
		NewMetricsDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &RelaydProvider{version: version} }
}
