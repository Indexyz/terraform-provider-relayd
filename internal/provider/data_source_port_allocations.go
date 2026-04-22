// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &PortAllocationsDataSource{}
var _ datasource.DataSourceWithConfigure = &PortAllocationsDataSource{}

type PortAllocationsDataSource struct {
	client *relaydClient
}

type PortAllocationsDataSourceModel struct {
	Allocations types.List `tfsdk:"allocations"`
}

func NewPortAllocationsDataSource() datasource.DataSource {
	return &PortAllocationsDataSource{}
}

func (d *PortAllocationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port_allocations"
}

func (d *PortAllocationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Lists relayd port allocations.",
		Attributes: map[string]datasourceschema.Attribute{
			"allocations": datasourceschema.ListNestedAttribute{
				MarkdownDescription: "Current relayd port allocations in protocol-then-port order.",
				Computed:            true,
				NestedObject: datasourceschema.NestedAttributeObject{
					Attributes: map[string]datasourceschema.Attribute{
						"id":                    datasourceschema.StringAttribute{Computed: true},
						"protocol":              datasourceschema.StringAttribute{Computed: true},
						"port":                  datasourceschema.Int64Attribute{Computed: true},
						"target_port":           datasourceschema.Int64Attribute{Computed: true},
						"host":                  datasourceschema.StringAttribute{Computed: true},
						"effective_target_port": datasourceschema.Int64Attribute{Computed: true},
						"effective_host":        datasourceschema.StringAttribute{Computed: true},
						"host_configured":       datasourceschema.BoolAttribute{Computed: true},
						"runtime_status":        datasourceschema.StringAttribute{Computed: true},
						"error_kind":            datasourceschema.StringAttribute{Computed: true},
						"last_error":            datasourceschema.StringAttribute{Computed: true},
						"created_at_ms":         datasourceschema.Int64Attribute{Computed: true},
						"updated_at_ms":         datasourceschema.Int64Attribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *PortAllocationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *PortAllocationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	allocations, err := d.client.ListAllocations(ctx)
	if err != nil {
		addError(&resp.Diagnostics, "Unable to list relayd port allocations", err)
		return
	}

	objects := make([]attr.Value, 0, len(allocations))
	for _, allocation := range allocations {
		objectValue, diags := allocationToObjectValue(ctx, allocation)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		objects = append(objects, objectValue)
	}

	listValue, diags := types.ListValue(types.ObjectType{AttrTypes: allocationObjectTypes}, objects)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := PortAllocationsDataSourceModel{Allocations: listValue}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
