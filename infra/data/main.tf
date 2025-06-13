# DTM by LEON LIN

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.39.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = ">= 6.39.0"
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
  project            = local.project_id
  service            = each.key
  disable_on_destroy = true
  disable_dependent_services=true
}

resource "google_sql_database_instance" "default" {
  # --- 基本設定 ---
  project          = local.project_id
  name             = "dtm-db"
  region           = local.region
  database_version = "POSTGRES_17"

  # --- 保護機制 ---
  # 若設為 true，可防止意外刪除執行個體
  deletion_protection = true

  settings {
    # --- 機器規格與可用性 ---
    tier              = "db-f1-micro"
    availability_type = "ZONAL" # ZONAL 表示單一區域，REGIONAL 表示高可用性
    edition           = "ENTERPRISE"
    
    location_preference {
      zone = local.zone
    }

    # --- 儲存空間設定 ---
    disk_type         = "PD_SSD" # 磁碟類型
    disk_size         = 10       # 磁碟大小 (GB)
    disk_autoresize   = false    # 停用儲存空間自動成長

    # --- 網路設定 ---
    ip_configuration {
      ipv4_enabled = true # 啟用 Public IP
    }
    
    # --- 備份設定 ---
    # 您提供的設定中，備份是停用的 (enabled = false)
    backup_configuration {
      enabled                        = false
      point_in_time_recovery_enabled = false # 停用 Point-in-Time Recovery
      
      # 以下設定僅在 enabled = true 時生效
      backup_retention_settings {
        retained_backups = 7
        retention_unit   = "COUNT"
      }
    }

    # --- 資料庫旗標 (Flags) ---
    # 啟用 IAM 資料庫身分驗證
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
    github_actions_builder   = google_service_account.github_actions_builder.email
  }
}

output "project_id_in_use" {
  description = "The GCP Project ID being managed by this Terraform configuration."
  value       = local.project_id
}

output "sql_connection_name" {
  description = "cloud sql connextion name."
  value = google_sql_database_instance.default.connection_name
}