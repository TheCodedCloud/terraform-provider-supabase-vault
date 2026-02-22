// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/jackc/pgx/v5"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &VaultSecretResource{}
var _ resource.ResourceWithImportState = &VaultSecretResource{}

func NewVaultSecretResource() resource.Resource {
	return &VaultSecretResource{}
}

// VaultSecretResource defines the resource implementation.
type VaultSecretResource struct {
	providerData *ProviderData
}

// VaultSecretModel describes the resource data model.
type VaultSecretModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Value       types.String `tfsdk:"value"`
	KeyID       types.String `tfsdk:"key_id"`
	Description types.String `tfsdk:"description"`
}

func (r *VaultSecretResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *VaultSecretResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a secret in Supabase Vault. Secrets are encrypted and stored securely in the database.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Secret UUID returned from vault functions",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Unique name for the secret",
				Required:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "Secret value to encrypt and store",
				Required:            true,
				Sensitive:           true,
			},
			"key_id": schema.StringAttribute{
				MarkdownDescription: "Optional encryption key ID (if using custom keys). This value is read from the database and preserved even if not specified in the configuration.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Optional description for the secret",
				Optional:            true,
			},
		},
	}
}

func (r *VaultSecretResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*ProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *ProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.providerData = providerData
}

// appendManagedByFooter appends a footer to the description indicating the secret is managed by Terraform.
func appendManagedByFooter(description string, version string) string {
	footer := fmt.Sprintf("\n\n---\nManaged by terraform-provider-supabase-vault v%s", version)

	if description == "" {
		return strings.TrimPrefix(footer, "\n\n")
	}

	return description + footer
}

func (r *VaultSecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VaultSecretModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Prepare description with footer
	description := ""
	if !data.Description.IsNull() {
		description = data.Description.ValueString()
	}
	descriptionWithFooter := appendManagedByFooter(description, r.providerData.Version)

	// Prepare the vault.create_secret() function call
	// vault.create_secret(secret_value, name, description)
	var secretID string
	var err error

	if !data.KeyID.IsNull() {
		// If key_id is provided, we need to use a different approach
		// Since vault.create_secret doesn't directly accept key_id, we'll need to handle this
		// For now, create without key_id and note that key_id support may need custom SQL
		tflog.Warn(ctx, "key_id parameter is not yet fully supported in create operation")
	}

	// Call vault.create_secret() using prepared statement
	// vault.create_secret returns a UUID directly (not a record)
	query := "SELECT vault.create_secret($1, $2, $3)"
	err = r.providerData.Pool.QueryRow(ctx, query,
		data.Value.ValueString(),
		data.Name.ValueString(),
		descriptionWithFooter,
	).Scan(&secretID)

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create vault secret",
			fmt.Sprintf("Error calling vault.create_secret: %s", err),
		)
		return
	}

	// Set the ID from the returned UUID
	data.ID = types.StringValue(secretID)

	// Read key_id from database to ensure it's a known value (computed attribute)
	keyIDQuery := `SELECT key_id FROM vault.secrets WHERE id = $1`
	var keyID sql.NullString
	err = r.providerData.Pool.QueryRow(ctx, keyIDQuery, secretID).Scan(&keyID)
	if err != nil {
		// If we can't read key_id, set it to null (better than unknown)
		data.KeyID = types.StringNull()
		tflog.Warn(ctx, "Unable to read key_id after creation, setting to null", map[string]interface{}{
			"error": err,
		})
	} else {
		if keyID.Valid {
			data.KeyID = types.StringValue(keyID.String)
		} else {
			data.KeyID = types.StringNull()
		}
	}

	tflog.Trace(ctx, "created a vault secret", map[string]interface{}{
		"id":   secretID,
		"name": data.Name.ValueString(),
	})

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VaultSecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data VaultSecretModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Query metadata directly from vault.secrets table (no decryption needed)
	// name, description, and key_id are stored as plaintext in vault.secrets
	// This is much more efficient than using vault.decrypted_secrets view
	query := `
		SELECT id, name, description, key_id 
		FROM vault.secrets 
		WHERE id = $1
	`

	var id, name, description string
	var keyID sql.NullString
	err := r.providerData.Pool.QueryRow(ctx, query, data.ID.ValueString()).Scan(
		&id, &name, &description, &keyID,
	)

	if err == pgx.ErrNoRows {
		// Secret not found, mark as removed
		resp.State.RemoveResource(ctx)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read vault secret metadata",
			fmt.Sprintf("Error reading secret metadata: %s", err),
		)
		return
	}

	// Update state with metadata (but not the secret value - it stays in state)
	data.Name = types.StringValue(name)
	if keyID.Valid {
		data.KeyID = types.StringValue(keyID.String)
	} else {
		data.KeyID = types.StringNull()
	}

	// Remove the managed-by footer from description if present.
	// This allows users to see their original description.
	if description != "" {
		footer := fmt.Sprintf("\n\n---\nManaged by terraform-provider-supabase-vault v%s", r.providerData.Version)
		description = strings.TrimSuffix(description, footer)
		data.Description = types.StringValue(description)
	} else {
		data.Description = types.StringNull()
	}

	// Note: We do NOT read the secret value for security reasons
	// The value remains in Terraform state and will be overwritten on update

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VaultSecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data VaultSecretModel
	var state VaultSecretModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Prepare description with footer
	description := ""
	if !data.Description.IsNull() {
		description = data.Description.ValueString()
	}
	descriptionWithFooter := appendManagedByFooter(description, r.providerData.Version)

	// Call vault.update_secret() using prepared statement
	// vault.update_secret(id, secret_value, name, description)
	query := "SELECT vault.update_secret($1, $2, $3, $4)"
	_, err := r.providerData.Pool.Exec(ctx, query,
		state.ID.ValueString(), // Use ID from state
		data.Value.ValueString(),
		data.Name.ValueString(),
		descriptionWithFooter,
	)

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to update vault secret",
			fmt.Sprintf("Error calling vault.update_secret: %s", err),
		)
		return
	}

	tflog.Trace(ctx, "updated a vault secret", map[string]interface{}{
		"id":   state.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VaultSecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VaultSecretModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Delete the secret using direct SQL (no helper function available)
	query := "DELETE FROM vault.secrets WHERE id = $1"
	_, err := r.providerData.Pool.Exec(ctx, query, data.ID.ValueString())

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to delete vault secret",
			fmt.Sprintf("Error deleting secret: %s", err),
		)
		return
	}

	tflog.Trace(ctx, "deleted a vault secret", map[string]interface{}{
		"id": data.ID.ValueString(),
	})
}

func (r *VaultSecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import by secret name - we'll need to look up the ID
	secretName := req.ID

	query := `
		SELECT id 
		FROM vault.decrypted_secrets 
		WHERE name = $1
	`

	var secretID string
	err := r.providerData.Pool.QueryRow(ctx, query, secretName).Scan(&secretID)

	if err == pgx.ErrNoRows {
		resp.Diagnostics.AddError(
			"Secret not found",
			fmt.Sprintf("No secret found with name: %s", secretName),
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to import vault secret",
			fmt.Sprintf("Error looking up secret by name: %s", err),
		)
		return
	}

	// Set the ID so Terraform can read the resource
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), secretID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), secretName)...)
}
