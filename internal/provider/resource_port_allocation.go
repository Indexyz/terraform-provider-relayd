// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &PortAllocationResource{}
var _ resource.ResourceWithConfigure = &PortAllocationResource{}
var _ resource.ResourceWithImportState = &PortAllocationResource{}

type PortAllocationResource struct{ client *relaydClient }

type PortAllocationResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Protocol    types.String `tfsdk:"protocol"`
	Port        types.Int64  `tfsdk:"port"`
	CreatedAtMs types.Int64  `tfsdk:"created_at_ms"`
	UpdatedAtMs types.Int64  `tfsdk:"updated_at_ms"`
}

func NewPortAllocationResource() resource.Resource { return &PortAllocationResource{} }

func (r *PortAllocationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port_allocation"
}

func (r *PortAllocationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		MarkdownDescription: "Manages a relayd allocation that reserves a listen port.",
		Attributes: map[string]resourceschema.Attribute{
			"id":            resourceschema.StringAttribute{MarkdownDescription: "Server-generated allocation identifier.", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"protocol":      resourceschema.StringAttribute{MarkdownDescription: "Forwarding protocol. Supported values are `tcp` and `udp`.", Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"port":          resourceschema.Int64Attribute{MarkdownDescription: "Allocated relay listen port.", Computed: true},
			"created_at_ms": resourceschema.Int64Attribute{MarkdownDescription: "Creation timestamp in Unix milliseconds.", Computed: true},
			"updated_at_ms": resourceschema.Int64Attribute{MarkdownDescription: "Last update timestamp in Unix milliseconds.", Computed: true},
		},
	}
}

func (r *PortAllocationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*relaydClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected resource configure type", fmt.Sprintf("Expected *relaydClient, got %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *PortAllocationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PortAllocationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validateAllocationModel(plan, &resp.Diagnostics) {
		return
	}

	allocation, err := r.client.CreateAllocation(ctx, createAllocationRequest{Protocol: strings.TrimSpace(plan.Protocol.ValueString())})
	if err != nil {
		addError(&resp.Diagnostics, "Unable to create relayd allocation", err)
		return
	}
	state := allocationToResourceModel(allocation)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PortAllocationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PortAllocationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	allocation, err := r.client.GetAllocation(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addError(&resp.Diagnostics, "Unable to read relayd allocation", err)
		return
	}
	updatedState := allocationToResourceModel(allocation)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *PortAllocationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state PortAllocationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	allocation, err := r.client.GetAllocation(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addError(&resp.Diagnostics, "Unable to refresh relayd allocation", err)
		return
	}
	updatedState := allocationToResourceModel(allocation)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *PortAllocationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PortAllocationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAllocation(ctx, state.ID.ValueString()); err != nil {
		addError(&resp.Diagnostics, "Unable to delete relayd allocation", err)
	}
}

func (r *PortAllocationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func allocationToResourceModel(allocation relaydAllocation) PortAllocationResourceModel {
	return PortAllocationResourceModel{ID: types.StringValue(allocation.ID), Protocol: types.StringValue(allocation.Protocol), Port: types.Int64Value(allocation.Port), CreatedAtMs: types.Int64Value(allocation.CreatedAtMs), UpdatedAtMs: types.Int64Value(allocation.UpdatedAtMs)}
}

func validateAllocationModel(model PortAllocationResourceModel, diags *diag.Diagnostics) bool {
	if model.Protocol.IsNull() || model.Protocol.IsUnknown() {
		diags.AddError("Missing protocol", "protocol must be configured.")
		return false
	}
	protocol := strings.TrimSpace(model.Protocol.ValueString())
	if protocol != "tcp" && protocol != "udp" {
		diags.AddError("Invalid protocol", "protocol must be either `tcp` or `udp`.")
		return false
	}
	return true
}
