terraform-import:
	@echo "Importing VPC network..."
	terraform import google_compute_network.vpc projects/project-vdr-bwc/global/networks/bwc-vpc

	@echo "Importing Subnet..."
	terraform import google_compute_subnetwork.subnet projects/project-vdr-bwc/regions/us-central1/subnetworks/bwc-subnet

	@echo "Importing GKE Cluster..."
	terraform import google_container_cluster.primary projects/project-vdr-bwc/locations/us-central1-a/clusters/bwc-cluster

	@echo "Importing GKE Node Pool..."
	terraform import google_container_node_pool.primary_nodes projects/project-vdr-bwc/locations/us-central1-a/clusters/bwc-cluster/nodePools/bwc-node-pool
