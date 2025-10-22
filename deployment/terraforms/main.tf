terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
  required_version = ">= 1.3.0"
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# ---- VPC & Subnet ----
resource "google_compute_network" "vpc" {
  name                    = "segment-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "subnet" {
  name          = "segment-subnet"
  ip_cidr_range = "10.0.0.0/16"
  region        = var.region
  network       = google_compute_network.vpc.id
}

# ---- GKE Cluster ----
resource "google_container_cluster" "primary" {
  name     = "segment-cluster"
  location = var.zone

  network    = google_compute_network.vpc.name
  subnetwork = google_compute_subnetwork.subnet.name

  remove_default_node_pool = true  # we manage node pool separately
  initial_node_count       = 1     # required but ignored

  deletion_protection = false
  ip_allocation_policy {}
}

# ---- Node Pool ----
resource "google_container_node_pool" "primary_nodes" {
  name     = "segment-node-pool"
  location = var.zone
  cluster  = google_container_cluster.primary.name

  node_count = 20

  node_config {
    machine_type = "e2-standard-4"
    disk_size_gb = 100            # 👈 100 GB per node
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]
    labels = {
      env = "dev"
    }
    tags = ["segment-node"]
  }
}

# ---- Variables ----
variable "project_id" {
  default = "project-vdr-bwc"
}
variable "region" {
  default = "asia-southeast1"
}
variable "zone" {
  default = "asia-southeast1"
}
