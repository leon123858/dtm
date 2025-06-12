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
  init_image  = "nginx" 
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
    "run.googleapis.com", "sqladmin.googleapis.com", "pubsub.googleapis.com",
    "compute.googleapis.com", "secretmanager.googleapis.com", "iamcredentials.googleapis.com"
  ])
  project            = local.project_id
  service            = each.key
  disable_on_destroy = true
  disable_dependent_services=true
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

# --- Backend 服務帳戶 (有權限) ---
resource "google_service_account" "backend_app_runtime" {
  account_id   = "dtm-backend-runtime"
  display_name = "DTM Backend App Runtime SA"
  description  = "Service account for the DTM backend with DB/PubSub access"
}

resource "google_project_iam_member" "sql_client_binding" {
  project = local.project_id
  role    = "roles/cloudsql.client"
  member  = google_service_account.backend_app_runtime.member
}

resource "google_project_iam_member" "pubsub_publisher_binding" {
  project = local.project_id
  role    = "roles/pubsub.publisher"
  member  = google_service_account.backend_app_runtime.member
}

resource "google_project_iam_member" "pubsub_subscriber_binding" {
  project = local.project_id
  role    = "roles/pubsub.subscriber"
  member  = google_service_account.backend_app_runtime.member
}

# --- Frontend 服務帳戶 (無權限) ---
resource "google_service_account" "frontend_app_runtime" {
  account_id   = "dtmf-frontend-runtime"
  display_name = "DTMF Frontend App Runtime SA"
  description  = "Service account for the DTM frontend with no permissions"
}

resource "google_cloud_run_v2_service" "dtm_backend" {
  name     = "dtm"
  location = local.region
  deletion_protection = false
  
  template {
    # 後端服務使用 "有權限" 的服務帳戶
    service_account = google_service_account.backend_app_runtime.email

    scaling {
      max_instance_count = 5
      min_instance_count = 0
    }

    containers {
      image   = local.init_image
      ports {
        container_port = 80
      }
    }
  }
  depends_on = [google_project_service.apis]
}

resource "google_cloud_run_v2_service" "dtmf_frontend" {
  name     = "dtmf"
  location = local.region
  deletion_protection = false

  template {
    # 前端服務使用 "無權限" 的服務帳戶
    service_account = google_service_account.frontend_app_runtime.email

    scaling {
      max_instance_count = 5
      min_instance_count = 0
    }

    containers {
      image = local.init_image
      ports {
        container_port = 80
      }
    }
  }
  depends_on = [google_project_service.apis]
}

resource "google_cloud_run_v2_service_iam_member" "dtm_backend_public_access" {
  project  = google_cloud_run_v2_service.dtm_backend.project
  location = google_cloud_run_v2_service.dtm_backend.location
  name     = google_cloud_run_v2_service.dtm_backend.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service_iam_member" "dtmf_frontend_public_access" {
  project  = google_cloud_run_v2_service.dtmf_frontend.project
  location = google_cloud_run_v2_service.dtmf_frontend.location
  name     = google_cloud_run_v2_service.dtmf_frontend.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

output "artifact_registry_repositories" {
  description = "The created Artifact Registry repository names."
  value = {
    backend  = google_artifact_registry_repository.backend_repo.name
    frontend = google_artifact_registry_repository.frontend_repo.name
  }
}

output "cloud_run_urls" {
  description = "The URLs of the deployed Cloud Run services."
  value = {
    backend_url  = google_cloud_run_v2_service.dtm_backend.uri
    frontend_url = google_cloud_run_v2_service.dtmf_frontend.uri
  }
}

output "service_account_emails" {
  description = "Emails of the created service accounts."
  value = {
    github_actions_builder   = google_service_account.github_actions_builder.email
    backend_application_runtime  = google_service_account.backend_app_runtime.email
    frontend_application_runtime = google_service_account.frontend_app_runtime.email
  }
}

output "project_id_in_use" {
  description = "The GCP Project ID being managed by this Terraform configuration."
  value       = local.project_id
}