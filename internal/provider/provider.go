// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Ensure SupabaseVaultProvider satisfies various provider interfaces.
var _ provider.Provider = &SupabaseVaultProvider{}
var _ provider.ProviderWithFunctions = &SupabaseVaultProvider{}
var _ provider.ProviderWithEphemeralResources = &SupabaseVaultProvider{}

// SupabaseVaultProvider defines the provider implementation.
type SupabaseVaultProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// SupabaseVaultProviderModel describes the provider data model.
type SupabaseVaultProviderModel struct {
	Host     types.String `tfsdk:"host"`
	Port     types.Int64  `tfsdk:"port"`
	Database types.String `tfsdk:"database"`
	User     types.String `tfsdk:"user"`
	Password types.String `tfsdk:"password"`
	SSLMode  types.String `tfsdk:"sslmode"`
}

// ProviderData holds the connection pool and version for resources.
type ProviderData struct {
	Pool    *pgxpool.Pool
	Version string
}

func (p *SupabaseVaultProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "supabase-vault"
	resp.Version = p.version
}

func (p *SupabaseVaultProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				MarkdownDescription: "PostgreSQL host address",
				Required:            true,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "PostgreSQL port number",
				Optional:            true,
			},
			"database": schema.StringAttribute{
				MarkdownDescription: "PostgreSQL database name (defaults to 'postgres')",
				Optional:            true,
			},
			"user": schema.StringAttribute{
				MarkdownDescription: "PostgreSQL user (defaults to 'postgres')",
				Optional:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "PostgreSQL password",
				Required:            true,
				Sensitive:           true,
			},
			"sslmode": schema.StringAttribute{
				MarkdownDescription: "PostgreSQL SSL mode (require, verify-full, etc.). If not specified, Supabase will use its default SSL configuration.",
				Optional:            true,
			},
		},
	}
}

func (p *SupabaseVaultProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data SupabaseVaultProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Set defaults
	port := int64(5432)
	if !data.Port.IsNull() {
		port = data.Port.ValueInt64()
	}

	database := "postgres"
	if !data.Database.IsNull() {
		database = data.Database.ValueString()
	}

	user := "postgres"
	if !data.User.IsNull() {
		user = data.User.ValueString()
	}

	// Strip protocol prefix from host if present (e.g., https:// or http://)
	host := data.Host.ValueString()
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "postgres://")
	host = strings.TrimPrefix(host, "postgresql://")
	// Remove trailing slash if present
	host = strings.TrimSuffix(host, "/")

	// Parse host to extract just the hostname (in case port/database are included)
	// Handle formats like: hostname, hostname:port, hostname:port/database
	hostname := host
	parsedPort := port
	parsedDatabase := database

	// Check if host contains port (format: hostname:port or hostname:port/database)
	if strings.Contains(host, ":") {
		parts := strings.SplitN(host, ":", 2)
		hostname = parts[0]
		remaining := parts[1]

		// Check if remaining part contains database (format: port/database)
		if strings.Contains(remaining, "/") {
			dbParts := strings.SplitN(remaining, "/", 2)
			if portStr := dbParts[0]; portStr != "" {
				// Port is already in host, use it
				if parsedPortInt, err := strconv.ParseInt(portStr, 10, 64); err == nil {
					parsedPort = parsedPortInt
				}
			}
			if dbName := dbParts[1]; dbName != "" {
				// Database is already in host, use it
				parsedDatabase = dbName
			}
		} else {
			// Only port, no database
			if portStr := remaining; portStr != "" {
				if parsedPortInt, err := strconv.ParseInt(portStr, 10, 64); err == nil {
					parsedPort = parsedPortInt
				}
			}
		}
	} else if strings.Contains(host, "/") {
		// Host contains database but no port (format: hostname/database)
		parts := strings.SplitN(host, "/", 2)
		hostname = parts[0]
		if dbName := parts[1]; dbName != "" {
			parsedDatabase = dbName
		}
	}

	// Build connection string
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s",
		url.QueryEscape(user),
		url.QueryEscape(data.Password.ValueString()),
		hostname,
		parsedPort,
		parsedDatabase,
	)

	// Only add sslmode if explicitly provided
	if !data.SSLMode.IsNull() {
		connString += fmt.Sprintf("?sslmode=%s", url.QueryEscape(data.SSLMode.ValueString()))
	}

	// Create connection pool (needed for concurrent Terraform operations)
	connectCtx, connectCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connectCancel()

	pool, err := pgxpool.New(connectCtx, connString)
	if err != nil {
		if connectCtx.Err() == context.DeadlineExceeded {
			resp.Diagnostics.AddError(
				"Unable to connect to PostgreSQL",
				"Connection timeout: unable to create connection pool within 10 seconds. Please check your connection settings and network connectivity.",
			)
		} else {
			resp.Diagnostics.AddError(
				"Unable to connect to PostgreSQL",
				fmt.Sprintf("Unable to create connection pool: %s", err),
			)
		}
		return
	}

	// Test the connection with a timeout
	pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
	defer pingCancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		if pingCtx.Err() == context.DeadlineExceeded {
			resp.Diagnostics.AddError(
				"Unable to connect to PostgreSQL",
				"Connection timeout: unable to ping database within 10 seconds. Please check your connection settings and network connectivity.",
			)
		} else {
			resp.Diagnostics.AddError(
				"Unable to connect to PostgreSQL",
				fmt.Sprintf("Unable to ping database: %s", err),
			)
		}
		return
	}

	tflog.Info(ctx, "Successfully connected to PostgreSQL database")

	// Store provider data
	providerData := &ProviderData{
		Pool:    pool,
		Version: p.version,
	}

	resp.DataSourceData = providerData
	resp.ResourceData = providerData
}

func (p *SupabaseVaultProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewVaultSecretResource,
	}
}

func (p *SupabaseVaultProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{
		// No ephemeral resources for MVP
	}
}

func (p *SupabaseVaultProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		// No data sources for MVP
	}
}

func (p *SupabaseVaultProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{
		// No functions for MVP
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &SupabaseVaultProvider{
			version: version,
		}
	}
}
