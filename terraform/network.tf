# The services run in the account's default VPC
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# Gateway load balancer: open to the internet on port 80
resource "aws_security_group" "alb" {
  name        = "file-transfer-alb"
  description = "HTTP to the gateway load balancer"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [var.alb_ingress_cidr]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# Gateway tasks: only the load balancer may reach them
resource "aws_security_group" "gateway" {
  name        = "file-transfer-gateway"
  description = "Gateway tasks"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description     = "HTTP from the load balancer"
    from_port       = 8000
    to_port         = 8000
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# Backend tasks: reachable only from inside the VPC. The NLB target group turns client IP
# preservation off, so requests and health checks arrive from the NLB's private addresses.
resource "aws_security_group" "backend" {
  name        = "file-transfer-backend"
  description = "Backend tasks"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "File requests (UDP) through the NLB"
    from_port   = 8080
    to_port     = 8080
    protocol    = "udp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
  }
  ingress {
    description = "Health check (TCP) from the NLB"
    from_port   = 8081
    to_port     = 8081
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
