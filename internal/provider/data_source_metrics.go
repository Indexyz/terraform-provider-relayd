// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &MetricsDataSource{}
var _ datasource.DataSourceWithConfigure = &MetricsDataSource{}

type MetricsDataSource struct {
	client *relaydClient
}

type MetricsDataSourceModel struct {
	Metrics types.Map `tfsdk:"metrics"`
}

func NewMetricsDataSource() datasource.DataSource {
	return &MetricsDataSource{}
}

func (d *MetricsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metrics"
}

func (d *MetricsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Reads relayd runtime metrics as an integer map.",
		Attributes: map[string]datasourceschema.Attribute{
			"metrics": datasourceschema.MapAttribute{
				MarkdownDescription: "Current relayd runtime metrics keyed by metric name.",
				Computed:            true,
				ElementType:         types.Int64Type,
			},
		},
	}
}

func (d *MetricsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*relaydClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected data source configure type",
			fmt.Sprintf("Expected *relaydClient, got %T.", req.ProviderData),
		)
		return
	}

	d.client = client
}

func (d *MetricsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	metrics, err := d.client.GetMetrics(ctx)
	if err != nil {
		addError(&resp.Diagnostics, "Unable to read relayd metrics", err)
		return
	}

	metricsValue, diags := types.MapValueFrom(ctx, types.Int64Type, metrics)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := MetricsDataSourceModel{Metrics: metricsValue}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
