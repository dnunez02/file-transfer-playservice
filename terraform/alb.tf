# Public load balancer in front of the gateway tasks
resource "aws_lb" "gateway" {
  name               = "file-transfer-gateway"
  load_balancer_type = "application"
  subnets            = data.aws_subnets.default.ids
  security_groups    = [aws_security_group.alb.id]
}

resource "aws_lb_target_group" "gateway" {
  name                 = "file-transfer-gateway"
  port                 = 8000
  protocol             = "HTTP"
  target_type          = "ip" # Fargate tasks register by IP address
  vpc_id               = data.aws_vpc.default.id
  deregistration_delay = 60 # at least the gateway's 5s drain plus a typical transfer

  health_check {
    path                = "/health" # 200 while serving, 503 once the task is draining
    matcher             = "200"
    interval            = 10
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

# HTTPS needs a domain and certificate, so this listener is plain HTTP
resource "aws_lb_listener" "gateway" {
  load_balancer_arn = aws_lb.gateway.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.gateway.arn
  }
}
