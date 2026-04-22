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

type PortAllocationResource struct {
	client *relaydClient
}

type PortAllocationResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Protocol            types.String `tfsdk:"protocol"`
	TargetPort          types.Int64  `tfsdk:"target_port"`
	Host                types.String `tfsdk:"host"`
	Port                types.Int64  `tfsdk:"port"`
	EffectiveTargetPort types.Int64  `tfsdk:"effective_target_port"`
	EffectiveHost       types.String `tfsdk:"effective_host"`
	HostConfigured      types.Bool   `tfsdk:"host_configured"`
	RuntimeStatus       types.String `tfsdk:"runtime_status"`
	ErrorKind           types.String `tfsdk:"error_kind"`
	LastError           types.String `tfsdk:"last_error"`
	CreatedAtMs         types.Int64  `tfsdk:"created_at_ms"`
	UpdatedAtMs         types.Int64  `tfsdk:"updated_at_ms"`
}

func NewPortAllocationResource() resource.Resource {
	return &PortAllocationResource{}
}

func (r *PortAllocationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port_allocation"
}

func (r *PortAllocationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		MarkdownDescription: "Manages a relayd port allocation.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				MarkdownDescription: "Server-generated allocation identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"protocol": resourceschema.StringAttribute{
				MarkdownDescription: "Forwarding protocol. Supported values are `tcp` and `udp`.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_port": resourceschema.Int64Attribute{
				MarkdownDescription: "Desired upstream target port.",
				Required:            true,
			},
			"host": resourceschema.StringAttribute{
				MarkdownDescription: "Desired upstream IP literal. Omit to leave the allocation in `rejecting_no_host` until a host is configured.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
							if req.PlanValue.IsNull() && !req.StateValue.IsNull() {
								resp.RequiresReplace = true
							}
						},
						"Replacing is required when removing an existing host configuration.",
						"Replacing is required when removing an existing host configuration.",
					),
				},
			},
			"port": resourceschema.Int64Attribute{
				MarkdownDescription: "Allocated relay listen port.",
				Computed:            true,
			},
			"effective_target_port": resourceschema.Int64Attribute{
				MarkdownDescription: "Runtime target port currently applied by relayd.",
				Computed:            true,
			},
			"effective_host": resourceschema.StringAttribute{
				MarkdownDescription: "Runtime host currently applied by relayd.",
				Computed:            true,
			},
			"host_configured": resourceschema.BoolAttribute{
				MarkdownDescription: "Whether a non-empty desired host is configured.",
				Computed:            true,
			},
			"runtime_status": resourceschema.StringAttribute{
				MarkdownDescription: "Runtime status reported by relayd.",
				Computed:            true,
			},
			"error_kind": resourceschema.StringAttribute{
				MarkdownDescription: "Runtime error kind reported by relayd, if any.",
				Computed:            true,
			},
			"last_error": resourceschema.StringAttribute{
				MarkdownDescription: "Last runtime error message reported by relayd, if any.",
				Computed:            true,
			},
			"created_at_ms": resourceschema.Int64Attribute{
				MarkdownDescription: "Creation timestamp in Unix milliseconds.",
				Computed:            true,
			},
			"updated_at_ms": resourceschema.Int64Attribute{
				MarkdownDescription: "Last update timestamp in Unix milliseconds.",
				Computed:            true,
			},
		},
	}
}

func (r *PortAllocationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*relaydClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *relaydClient, got %T.", req.ProviderData),
		)
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

	if !validateCreateOrUpdateModel(plan, &resp.Diagnostics) {
		return
	}

	protocol := strings.TrimSpace(plan.Protocol.ValueString())
	created, err := r.client.CreateAllocation(ctx, createPortAllocationRequest{
		Protocol:   protocol,
		TargetPort: plan.TargetPort.ValueInt64(),
	})
	if err != nil {
		addError(&resp.Diagnostics, "Unable to create relayd port allocation", err)
		return
	}

	current := created
	if !plan.Host.IsNull() {
		host := strings.TrimSpace(plan.Host.ValueString())
		updated, updateErr := r.client.UpdateAllocation(ctx, created.ID, updatePortAllocationRequest{Host: &host})
		if updateErr != nil {
			rollbackErr := r.client.DeleteAllocation(ctx, created.ID)
			if rollbackErr != nil {
				resp.Diagnostics.AddError(
					"Unable to finish creating relayd port allocation",
					fmt.Sprintf("Host assignment failed: %s. Rollback failed: %s", updateErr, rollbackErr),
				)
				return
			}

			resp.Diagnostics.AddError("Unable to finish creating relayd port allocation", updateErr.Error())
			return
		}
		current = updated
	}

	state := allocationToResourceModel(current)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PortAllocationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PortAllocationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	allocations, err := r.client.ListAllocations(ctx)
	if err != nil {
		addError(&resp.Diagnostics, "Unable to read relayd port allocations", err)
		return
	}

	allocation, found := findAllocationByID(allocations, state.ID.ValueString())
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	updatedState := allocationToResourceModel(allocation)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *PortAllocationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PortAllocationResourceModel
	var state PortAllocationResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !validateCreateOrUpdateModel(plan, &resp.Diagnostics) {
		return
	}

	updateReq := updatePortAllocationRequest{}
	if plan.TargetPort.ValueInt64() != state.TargetPort.ValueInt64() {
		targetPort := plan.TargetPort.ValueInt64()
		updateReq.TargetPort = &targetPort
	}

	if !plan.Host.Equal(state.Host) {
		host := strings.TrimSpace(plan.Host.ValueString())
		updateReq.Host = &host
	}

	if updateReq.TargetPort == nil && updateReq.Host == nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	updated, err := r.client.UpdateAllocation(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		addError(&resp.Diagnostics, "Unable to update relayd port allocation", err)
		return
	}

	updatedState := allocationToResourceModel(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *PortAllocationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PortAllocationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAllocation(ctx, state.ID.ValueString()); err != nil {
		addError(&resp.Diagnostics, "Unable to delete relayd port allocation", err)
	}
}

func (r *PortAllocationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func allocationToResourceModel(allocation relaydPortAllocation) PortAllocationResourceModel {
	return PortAllocationResourceModel{
		ID:                  types.StringValue(allocation.ID),
		Protocol:            types.StringValue(allocation.Protocol),
		TargetPort:          types.Int64Value(allocation.TargetPort),
		Host:                stringValueOrNull(allocation.Host),
		Port:                types.Int64Value(allocation.Port),
		EffectiveTargetPort: int64ValueOrNull(allocation.EffectiveTargetPort),
		EffectiveHost:       stringValueOrNull(allocation.EffectiveHost),
		HostConfigured:      types.BoolValue(allocation.HostConfigured),
		RuntimeStatus:       types.StringValue(allocation.RuntimeStatus),
		ErrorKind:           stringValueOrNull(allocation.ErrorKind),
		LastError:           stringValueOrNull(allocation.LastError),
		CreatedAtMs:         types.Int64Value(allocation.CreatedAtMs),
		UpdatedAtMs:         types.Int64Value(allocation.UpdatedAtMs),
	}
}

func validateCreateOrUpdateModel(model PortAllocationResourceModel, diags *diag.Diagnostics) bool {
	if model.Protocol.IsNull() || model.Protocol.IsUnknown() {
		diags.AddError("Missing protocol", "protocol must be configured.")
		return false
	}

	protocol := strings.TrimSpace(model.Protocol.ValueString())
	if protocol != "tcp" && protocol != "udp" {
		diags.AddError("Invalid protocol", "protocol must be either `tcp` or `udp`.")
		return false
	}

	if model.TargetPort.IsNull() || model.TargetPort.IsUnknown() {
		diags.AddError("Missing target_port", "target_port must be configured.")
		return false
	}

	if model.TargetPort.ValueInt64() <= 0 {
		diags.AddError("Invalid target_port", "target_port must be greater than zero.")
		return false
	}

	if !model.Host.IsNull() && strings.TrimSpace(model.Host.ValueString()) == "" {
		diags.AddError("Invalid host", "host cannot be an empty string. Omit the attribute to leave host unset.")
		return false
	}

	return true
}
