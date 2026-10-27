output "alb_dns_name" {
  value       = aws_lb.gateway.dns_name
  description = "Public address of the gateway: http://<this>/files?name=<file>"
}

output "backend_nlb_dns_name" {
  value       = aws_lb.backend.dns_name
  description = "Internal address of the backend load balancer (UDP 8080)"
}

output "files_bucket" {
  value       = data.aws_s3_bucket.files.bucket
  description = "Existing bucket the backend reads from"
}

output "files_prefix" {
  value       = var.files_prefix
  description = "Key prefix of the temporary files in the bucket"
}

output "ecr_gateway_url" {
  value = aws_ecr_repository.gateway.repository_url
}

output "ecr_backend_url" {
  value = aws_ecr_repository.backend.repository_url
}

output "ecs_cluster_name" {
  value = aws_ecs_cluster.main.name
}
