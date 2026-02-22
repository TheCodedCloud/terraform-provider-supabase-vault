provider "supabase-vault" {
  host     = "db.example.supabase.co"
  port     = 5432
  # database defaults to "postgres" if not specified
  # user defaults to "postgres" if not specified
  password = var.postgres_password
  # sslmode is optional - Supabase will use its default SSL configuration if not specified
}
