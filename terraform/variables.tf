variable "image_tag" {
  type        = string
  description = "Tag of the gateway and backend images in ECR"
  default     = "latest"
}

variable "files_bucket" {
  type        = string
  description = "Existing S3 bucket the backend reads files from (LabRole cannot create buckets)"
  default     = "file-transfer-718fc4054bb6e2394d91c1d3f2"
}

variable "files_prefix" {
  type        = string
  description = "Key prefix for the temporary files in the bucket (PREFIX); keep the trailing slash"
  default     = "tmp/"

  validation {
    condition     = var.files_prefix != "" && endswith(var.files_prefix, "/")
    error_message = "files_prefix must be non-empty and end with a slash."
  }
}

variable "gateway_min" {
  type        = number
  description = "Fewest gateway tasks"
  default     = 2
}

variable "gateway_max" {
  type        = number
  description = "Most gateway tasks"
  default     = 4
}

variable "backend_min" {
  type        = number
  description = "Fewest backend tasks"
  default     = 2
}

variable "backend_max" {
  type        = number
  description = "Most backend tasks"
  default     = 4
}

variable "alb_ingress_cidr" {
  type        = string
  description = "Who may reach the gateway load balancer on port 80"
  default     = "0.0.0.0/0"
}

variable "max_inflight" {
  type        = number
  description = "File transfers one gateway task runs at once (MAX_INFLIGHT)"
  default     = 4
}

variable "backend_workers" {
  type        = number
  description = "Requests one backend task handles at once (WORKERS)"
  default     = 8
}

variable "gateway_requests_per_target" {
  type        = number
  description = "Requests per minute per gateway task that the auto scaler aims for"
  default     = 100
}

variable "backend_cpu_target" {
  type        = number
  description = "Average backend CPU percent that the auto scaler aims for"
  default     = 60
}

variable "backend_send_loss" {
  type        = number
  description = "Percent of datagrams each backend task drops when sending, to simulate a lossy network (SEND_LOSS)"
  default     = 0

  validation {
    condition     = var.backend_send_loss >= 0 && var.backend_send_loss <= 100
    error_message = "backend_send_loss must be from 0 to 100."
  }
}

variable "backend_recv_loss" {
  type        = number
  description = "Percent of datagrams each backend task drops when receiving (RECV_LOSS)"
  default     = 0

  validation {
    condition     = var.backend_recv_loss >= 0 && var.backend_recv_loss <= 100
    error_message = "backend_recv_loss must be from 0 to 100."
  }
}
