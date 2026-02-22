resource "supabase-vault_secret" "api_key" {
  name        = "api_key"
  value       = "my-secret-api-key-value"
  description = "API key for external service"
}

resource "supabase-vault_secret" "database_password" {
  name  = "database_password"
  value = var.database_password
}
