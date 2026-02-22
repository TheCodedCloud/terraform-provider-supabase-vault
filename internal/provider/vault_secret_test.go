// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccVaultSecretResource(t *testing.T) {
	// Skip if TF_ACC is not set
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccVaultSecretResourceConfig("test-secret-1", "my-secret-value", "Test secret description"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("name"),
						knownvalue.StringExact("test-secret-1"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("value"),
						knownvalue.StringExact("my-secret-value"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("description"),
						knownvalue.StringExact("Test secret description"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("id"),
						knownvalue.NotNull(),
					),
				},
			},
			// ImportState testing
			{
				ResourceName:            "supabase-vault_secret.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"value"}, // Value is not read back for security
			},
			// Update and Read testing
			{
				Config: testAccVaultSecretResourceConfig("test-secret-1", "updated-secret-value", "Updated test secret description"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("name"),
						knownvalue.StringExact("test-secret-1"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("value"),
						knownvalue.StringExact("updated-secret-value"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("description"),
						knownvalue.StringExact("Updated test secret description"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("id"),
						knownvalue.NotNull(),
					),
				},
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccVaultSecretResource_Minimal(t *testing.T) {
	// Skip if TF_ACC is not set
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Acceptance tests skipped unless env 'TF_ACC' set")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with minimal config (only name and value)
			{
				Config: testAccVaultSecretResourceConfigMinimal("test-secret-minimal", "minimal-value"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("name"),
						knownvalue.StringExact("test-secret-minimal"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("value"),
						knownvalue.StringExact("minimal-value"),
					),
					statecheck.ExpectKnownValue(
						"supabase-vault_secret.test",
						tfjsonpath.New("id"),
						knownvalue.NotNull(),
					),
				},
			},
		},
	})
}

func testAccVaultSecretResourceConfig(name, value, description string) string {
	host := os.Getenv("SUPABASE_HOST")
	port := os.Getenv("SUPABASE_PORT")
	if port == "" {
		port = "5432"
	}
	database := os.Getenv("SUPABASE_DATABASE")
	if database == "" {
		database = "postgres"
	}
	user := os.Getenv("SUPABASE_USER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("SUPABASE_PASSWORD")
	sslmode := os.Getenv("SUPABASE_SSLMODE")

	config := fmt.Sprintf(`
provider "supabase-vault" {
  host     = %q
  password = %q
`, host, password)

	if port != "" {
		config += fmt.Sprintf(`  port     = %s
`, port)
	}
	if database != "" {
		config += fmt.Sprintf(`  database = %q
`, database)
	}
	if user != "" {
		config += fmt.Sprintf(`  user     = %q
`, user)
	}
	if sslmode != "" {
		config += fmt.Sprintf(`  sslmode  = %q
`, sslmode)
	}

	config += fmt.Sprintf(`}

resource "supabase-vault_secret" "test" {
  name        = %q
  value       = %q
  description = %q
}
`, name, value, description)

	return config
}

func testAccVaultSecretResourceConfigMinimal(name, value string) string {
	host := os.Getenv("SUPABASE_HOST")
	port := os.Getenv("SUPABASE_PORT")
	if port == "" {
		port = "5432"
	}
	database := os.Getenv("SUPABASE_DATABASE")
	if database == "" {
		database = "postgres"
	}
	user := os.Getenv("SUPABASE_USER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("SUPABASE_PASSWORD")
	sslmode := os.Getenv("SUPABASE_SSLMODE")

	config := fmt.Sprintf(`
provider "supabase-vault" {
  host     = %q
  password = %q
`, host, password)

	if port != "" {
		config += fmt.Sprintf(`  port     = %s
`, port)
	}
	if database != "" {
		config += fmt.Sprintf(`  database = %q
`, database)
	}
	if user != "" {
		config += fmt.Sprintf(`  user     = %q
`, user)
	}
	if sslmode != "" {
		config += fmt.Sprintf(`  sslmode  = %q
`, sslmode)
	}

	config += fmt.Sprintf(`}

resource "supabase-vault_secret" "test" {
  name  = %q
  value = %q
}
`, name, value)

	return config
}

