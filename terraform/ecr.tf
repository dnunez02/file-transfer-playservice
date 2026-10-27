# One image repository per container
resource "aws_ecr_repository" "gateway" {
  name         = "file-transfer-gateway"
  force_delete = true # lets terraform destroy remove a repository that still holds images
}

resource "aws_ecr_repository" "backend" {
  name         = "file-transfer-backend"
  force_delete = true
}

resource "aws_ecr_lifecycle_policy" "keep_last_five" {
  for_each   = { gateway = aws_ecr_repository.gateway.name, backend = aws_ecr_repository.backend.name }
  repository = each.value

  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the last 5 images"
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 5 }
      action       = { type = "expire" }
    }]
  })
}
