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

var _ resource.Resource = &PortBindingResource{}
var _ resource.ResourceWithConfigure = &PortBindingResource{}
var _ resource.ResourceWithImportState = &PortBindingResource{}

type PortBindingResource struct{ client *relaydClient }

type PortBindingResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	AllocationID        types.String `tfsdk:"allocation_id"`
	Host                types.String `tfsdk:"host"`
	TargetPort          types.Int64  `tfsdk:"target_port"`
	EffectiveTargetPort types.Int64  `tfsdk:"effective_target_port"`
	EffectiveHost       types.String `tfsdk:"effective_host"`
	RuntimeStatus       types.String `tfsdk:"runtime_status"`
	ErrorKind           types.String `tfsdk:"error_kind"`
	LastError           types.String `tfsdk:"last_error"`
	CreatedAtMs         types.Int64  `tfsdk:"created_at_ms"`
	UpdatedAtMs         types.Int64  `tfsdk:"updated_at_ms"`
}

func NewPortBindingResource() resource.Resource { return &PortBindingResource{} }

func (r *PortBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port_binding"
}

func (r *PortBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		MarkdownDescription: "Manages the binding attached to a relayd allocation.",
		Attributes: map[string]resourceschema.Attribute{
			"id":                    resourceschema.StringAttribute{MarkdownDescription: "Binding identifier, equal to the allocation identifier.", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"allocation_id":         resourceschema.StringAttribute{MarkdownDescription: "Identifier of the allocation to bind.", Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"host":                  resourceschema.StringAttribute{MarkdownDescription: "Desired upstream IP literal.", Required: true},
			"target_port":           resourceschema.Int64Attribute{MarkdownDescription: "Desired upstream target port.", Required: true},
			"effective_target_port": resourceschema.Int64Attribute{MarkdownDescription: "Runtime target port currently applied by relayd.", Computed: true},
			"effective_host":        resourceschema.StringAttribute{MarkdownDescription: "Runtime host currently applied by relayd.", Computed: true},
			"runtime_status":        resourceschema.StringAttribute{MarkdownDescription: "Runtime status reported by relayd.", Computed: true},
			"error_kind":            resourceschema.StringAttribute{MarkdownDescription: "Runtime error kind reported by relayd, if any.", Computed: true},
			"last_error":            resourceschema.StringAttribute{MarkdownDescription: "Last runtime error message reported by relayd, if any.", Computed: true},
			"created_at_ms":         resourceschema.Int64Attribute{MarkdownDescription: "Creation timestamp in Unix milliseconds.", Computed: true},
			"updated_at_ms":         resourceschema.Int64Attribute{MarkdownDescription: "Last update timestamp in Unix milliseconds.", Computed: true},
		},
	}
}

func (r *PortBindingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PortBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PortBindingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validateBindingModel(plan, &resp.Diagnostics) {
		return
	}

	binding, err := r.client.PutBinding(ctx, plan.AllocationID.ValueString(), putBindingRequest{Host: strings.TrimSpace(plan.Host.ValueString()), TargetPort: plan.TargetPort.ValueInt64()})
	if err != nil {
		addError(&resp.Diagnostics, "Unable to create relayd binding", err)
		return
	}
	state := bindingToResourceModel(binding)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PortBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PortBindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	binding, err := r.client.GetBinding(ctx, state.AllocationID.ValueString())
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addError(&resp.Diagnostics, "Unable to read relayd binding", err)
		return
	}
	updatedState := bindingToResourceModel(binding)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *PortBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PortBindingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validateBindingModel(plan, &resp.Diagnostics) {
		return
	}

	binding, err := r.client.PutBinding(ctx, plan.AllocationID.ValueString(), putBindingRequest{Host: strings.TrimSpace(plan.Host.ValueString()), TargetPort: plan.TargetPort.ValueInt64()})
	if err != nil {
		addError(&resp.Diagnostics, "Unable to update relayd binding", err)
		return
	}
	updatedState := bindingToResourceModel(binding)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *PortBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PortBindingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteBinding(ctx, state.AllocationID.ValueString()); err != nil {
		addError(&resp.Diagnostics, "Unable to delete relayd binding", err)
	}
}

func (r *PortBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resource.ImportStatePassthroughID(ctx, path.Root("allocation_id"), req, resp)
}

func bindingToResourceModel(binding relaydBinding) PortBindingResourceModel {
	return PortBindingResourceModel{
		ID:                  types.StringValue(binding.AllocationID),
		AllocationID:        types.StringValue(binding.AllocationID),
		Host:                types.StringValue(binding.Host),
		TargetPort:          types.Int64Value(binding.TargetPort),
		EffectiveTargetPort: int64ValueOrNull(binding.EffectiveTargetPort),
		EffectiveHost:       stringValueOrNull(binding.EffectiveHost),
		RuntimeStatus:       types.StringValue(binding.RuntimeStatus),
		ErrorKind:           stringValueOrNull(binding.ErrorKind),
		LastError:           stringValueOrNull(binding.LastError),
		CreatedAtMs:         types.Int64Value(binding.CreatedAtMs),
		UpdatedAtMs:         types.Int64Value(binding.UpdatedAtMs),
	}
}

func validateBindingModel(model PortBindingResourceModel, diags *diag.Diagnostics) bool {
	if model.AllocationID.IsNull() || model.AllocationID.IsUnknown() || strings.TrimSpace(model.AllocationID.ValueString()) == "" {
		diags.AddError("Missing allocation_id", "allocation_id must be configured.")
		return false
	}
	if model.Host.IsNull() || model.Host.IsUnknown() || strings.TrimSpace(model.Host.ValueString()) == "" {
		diags.AddError("Missing host", "host must be configured.")
		return false
	}
	if model.TargetPort.IsNull() || model.TargetPort.IsUnknown() || model.TargetPort.ValueInt64() <= 0 {
		diags.AddError("Invalid target_port", "target_port must be greater than zero.")
		return false
	}
	return true
}
