# Internal load balancer in front of the backend tasks. An ALB cannot balance UDP, so this is
# a network load balancer, and only the gateway tasks in the VPC can reach it.
resource "aws_lb" "backend" {
  name                             = "file-transfer-backend"
  load_balancer_type               = "network"
  internal                         = true
  subnets                          = data.aws_subnets.default.ids
  enable_cross_zone_load_balancing = true
}

resource "aws_lb_target_group" "backend" {
  name                 = "file-transfer-backend"
  port                 = 8080
  protocol             = "UDP"
  target_type          = "ip"
  vpc_id               = data.aws_vpc.default.id
  deregistration_delay = 60
  preserve_client_ip   = "true"

  # A UDP target group cannot health-check UDP, so it asks the server's TCP health port
  health_check {
    protocol            = "HTTP"
    port                = "8081"
    path                = "/health"
    interval            = 10
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_listener" "backend" {
  load_balancer_arn = aws_lb.backend.arn
  port              = 8080
  protocol          = "UDP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.backend.arn
  }
}
