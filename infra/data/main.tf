# DTM by LEON LIN

terraform {
  backend "gcs" {
    bucket = "my-terraform-state-division-trip-money-20250614"
    prefix = "terraform/state/data"
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.39.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = ">= 6.39.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.7"
    }
  }
}

locals {
  project_id = "division-trip-money"
  region     = "asia-east1"
  zone       = "asia-east1-b"
}

provider "google" {
  project = local.project_id
  region  = local.region
  zone    = local.zone
}

provider "google-beta" {
  project = local.project_id
  region  = local.region
  zone    = local.zone
}

resource "google_project_service" "apis" {
  for_each = toset([
    "run.googleapis.com",
    "sqladmin.googleapis.com",
    "pubsub.googleapis.com",
    "artifactregistry.googleapis.com",
    "compute.googleapis.com",
    "iamcredentials.googleapis.com"
  ])
  project                    = local.project_id
  service                    = each.key
  disable_on_destroy         = true
  disable_dependent_services = true
}

resource "random_password" "db_password" {
  length = 32


  lower   = true
  upper   = true
  numeric = true
  special = true


  min_lower   = 4
  min_upper   = 4
  min_numeric = 4
  min_special = 4


  override_special = "!@#$%^&*()-_=+[]{}"
}

resource "google_sql_database_instance" "default" {

  project          = local.project_id
  name             = "dtm-db"
  region           = local.region
  database_version = "POSTGRES_17"
  root_password    = random_password.db_password.result


  deletion_protection = false

  settings {

    tier              = "db-f1-micro"
    availability_type = "ZONAL"
    edition           = "ENTERPRISE"

    location_preference {
      zone = local.zone
    }


    disk_type       = "PD_SSD"
    disk_size       = 10
    disk_autoresize = false

    ip_configuration {
      ipv4_enabled = true # Public IP
    }

    backup_configuration {
      enabled                        = false
      point_in_time_recovery_enabled = false # Point-in-Time Recovery

      backup_retention_settings {
        retained_backups = 7
        retention_unit   = "COUNT"
      }
    }


    database_flags {
      name  = "cloudsql.iam_authentication"
      value = "on"
    }
  }
}

resource "google_artifact_registry_repository" "backend_repo" {
  provider      = google-beta
  location      = local.region
  repository_id = "backend"
  description   = "Repository for backend Docker images"
  format        = "DOCKER"
  depends_on    = [google_project_service.apis]
}

resource "google_artifact_registry_repository" "frontend_repo" {
  provider      = google-beta
  location      = local.region
  repository_id = "frontend"
  description   = "Repository for frontend Docker images"
  format        = "DOCKER"
  depends_on    = [google_project_service.apis]
}

resource "google_service_account" "github_actions_builder" {
  account_id   = "github-actions-builder"
  display_name = "GitHub Actions Builder SA"
  description  = "Service account for GitHub Actions to build and push Docker images"
}

resource "google_project_iam_member" "artifact_writer_binding" {
  project = local.project_id
  role    = "roles/artifactregistry.writer"
  member  = google_service_account.github_actions_builder.member
}

output "artifact_registry_repositories" {
  description = "The created Artifact Registry repository names."
  value = {
    backend  = google_artifact_registry_repository.backend_repo.name
    frontend = google_artifact_registry_repository.frontend_repo.name
  }
}

output "service_account_emails" {
  description = "Emails of the created service accounts."
  value = {
    github_actions_builder = google_service_account.github_actions_builder.email
  }
}

output "project_id_in_use" {
  description = "The GCP Project ID being managed by this Terraform configuration."
  value       = local.project_id
}

// use below code to read pwd
output "generated_db_password" {
  description = "can read by: terraform output -raw generated_db_password"
  value       = random_password.db_password.result
  sensitive   = true
}

output "sql_connection_name" {
  description = "cloud sql connextion name."
  value       = google_sql_database_instance.default.connection_name
}