# Gateway: add tasks when each one handles more than the target number of requests per minute
resource "aws_appautoscaling_target" "gateway" {
  service_namespace  = "ecs"
  scalable_dimension = "ecs:service:DesiredCount"
  resource_id        = "service/${aws_ecs_cluster.main.name}/${aws_ecs_service.gateway.name}"
  min_capacity       = var.gateway_min
  max_capacity       = var.gateway_max
}

resource "aws_appautoscaling_policy" "gateway" {
  name               = "gateway-requests-per-target"
  policy_type        = "TargetTrackingScaling"
  service_namespace  = aws_appautoscaling_target.gateway.service_namespace
  scalable_dimension = aws_appautoscaling_target.gateway.scalable_dimension
  resource_id        = aws_appautoscaling_target.gateway.resource_id

  target_tracking_scaling_policy_configuration {
    target_value       = var.gateway_requests_per_target
    scale_out_cooldown = 60
    scale_in_cooldown  = 120

    predefined_metric_specification {
      predefined_metric_type = "ALBRequestCountPerTarget"
      resource_label         = "${aws_lb.gateway.arn_suffix}/${aws_lb_target_group.gateway.arn_suffix}"
    }
  }
}

# Backend: a network load balancer has no per-target request metric, so scale on CPU
resource "aws_appautoscaling_target" "backend" {
  service_namespace  = "ecs"
  scalable_dimension = "ecs:service:DesiredCount"
  resource_id        = "service/${aws_ecs_cluster.main.name}/${aws_ecs_service.backend.name}"
  min_capacity       = var.backend_min
  max_capacity       = var.backend_max
}

resource "aws_appautoscaling_policy" "backend" {
  name               = "backend-cpu"
  policy_type        = "TargetTrackingScaling"
  service_namespace  = aws_appautoscaling_target.backend.service_namespace
  scalable_dimension = aws_appautoscaling_target.backend.scalable_dimension
  resource_id        = aws_appautoscaling_target.backend.resource_id

  target_tracking_scaling_policy_configuration {
    target_value       = var.backend_cpu_target
    scale_out_cooldown = 60
    scale_in_cooldown  = 120

    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
  }
}
