# DTM by LEON LIN

terraform {
  backend "gcs" {
    bucket = "my-terraform-state-division-trip-money-20250614"
    prefix = "terraform/state/server" # 可選：指定 state 文件在 bucket 中的路徑前綴
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
  }
}

locals {
  project_id = "division-trip-money"
  region     = "asia-east1"
  zone       = "asia-east1-b"
  DN_front   = "powerbunny.page"
  DN_back    = "dtm.powerbunny.xyz"
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

# auth proxy setting
# should also set it in cloud sql flag: "cloudsql.iam_authentication" = "on"
resource "google_project_iam_member" "sql_instance_user_binding" {
  project = local.project_id
  role    = "roles/cloudsql.instanceUser"
  member  = google_service_account.backend_app_runtime.member
}

# --- Frontend 服務帳戶 (無權限) ---
resource "google_service_account" "frontend_app_runtime" {
  account_id   = "dtmf-frontend-runtime"
  display_name = "DTMF Frontend App Runtime SA"
  description  = "Service account for the DTM frontend with no permissions"
}

resource "google_cloud_run_v2_service" "dtmf_frontend" {
  provider             = google-beta
  name                 = "dtmf"
  location             = local.region
  deletion_protection  = false
  default_uri_disabled = true

  template {
    # 前端服務使用 "無權限" 的服務帳戶
    service_account = google_service_account.frontend_app_runtime.email

    scaling {
      max_instance_count = 5
      min_instance_count = 0
    }

    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello:latest" # Image to deploy

      # startup_probe {
      #   initial_delay_seconds = 0
      #   period_seconds = 0
      #   timeout_seconds = 0
      #   failure_threshold = 0
      # }

      # liveness_probe {
      #   initial_delay_seconds = 0
      #   period_seconds = 0
      #   timeout_seconds = 0
      #   failure_threshold = 0
      # }

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }

        cpu_idle = "true"
      }

      # env {
      #   name  = "ADMIN_KEY"
      #   value = ""
      # }
    }
  }
}

resource "google_cloud_run_v2_service" "dtm_backend" {
  provider             = google-beta
  name                 = "dtm"
  location             = local.region
  deletion_protection  = false
  default_uri_disabled = true

  template {
    # 後端服務使用 "有權限" 的服務帳戶
    service_account = google_service_account.backend_app_runtime.email

    scaling {
      max_instance_count = 5
      min_instance_count = 0
    }

    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello:latest" # Image to deploy

      # startup_probe {
      #   initial_delay_seconds = 0
      #   period_seconds        = 0
      #   timeout_seconds       = 0
      #   failure_threshold     = 0
      # }

      # liveness_probe {
      #   initial_delay_seconds = 0
      #   period_seconds        = 0
      #   timeout_seconds       = 0
      #   failure_threshold     = 0
      # }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
        cpu_idle = "true"
      }

      env {
        name  = "DATABASE_PASSWORD"
        value = var.dtm-backend-db-password
      }
      env {
        name  = "DATABASE_HOST"
        value = format("%s/%s", "/cloudsql", var.dtm-backend-db-connection-name)
      }
      env {
        name  = "FRONTEND_URL"
        value = format("%s://%s", "https", local.DN_front)
      }
      # env {
      #   name = "ADMIN_KEY"
      #   value = ""
      # }
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [var.dtm-backend-db-connection-name]
      }
    }
  }

  depends_on = [google_cloud_run_v2_service.dtmf_frontend]
}

resource "google_cloud_run_domain_mapping" "backend" {
  name     = local.DN_back
  location = google_cloud_run_v2_service.dtm_backend.location
  metadata {
    namespace = local.project_id
  }
  spec {
    route_name = google_cloud_run_v2_service.dtm_backend.name
  }
}

resource "google_cloud_run_domain_mapping" "frontend" {
  name     = local.DN_front
  location = google_cloud_run_v2_service.dtmf_frontend.location
  metadata {
    namespace = local.project_id
  }
  spec {
    route_name = google_cloud_run_v2_service.dtmf_frontend.name
  }
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
    backend_application_runtime  = google_service_account.backend_app_runtime.email
    frontend_application_runtime = google_service_account.frontend_app_runtime.email
  }
}

output "project_id_in_use" {
  description = "The GCP Project ID being managed by this Terraform configuration."
  value       = local.project_id
}