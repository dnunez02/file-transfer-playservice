# The stack is split over several files in this folder:
#   network.tf      default VPC, subnets and security groups
#   ecr.tf          image repositories for the gateway and backend containers
#   ecs.tf          ECS cluster, task definitions and the gateway and backend services
#   alb.tf          public load balancer in front of the gateway
#   nlb.tf          internal UDP load balancer in front of the backend
#   autoscaling.tf  auto scalers for both services
#   variables.tf    settings you can override (see terraform.tfvars)
#   outputs.tf      addresses of the load balancers and image repositories
# This file holds the provider and the lookup of the existing S3 bucket the backend serves files from.

terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

# The provider of your cloud service, in this case it is AWS.
provider "aws" {
  region = "us-west-2" # Which region you are working on
}

# Existing bucket the server fetches requested files from. LabRole cannot manage buckets, so
# Terraform only looks it up; files live under var.files_prefix.
data "aws_s3_bucket" "files" {
  bucket = var.files_bucket
}
