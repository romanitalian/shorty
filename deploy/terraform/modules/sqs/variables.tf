# deploy/terraform/modules/sqs/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- Queue configuration ---

variable "queue_name" {
  description = "Base name for the FIFO queue (will append .fifo)"
  type        = string
  default     = "clicks"
}

variable "message_retention_seconds" {
  description = "Message retention period in seconds (default: 4 days)"
  type        = number
  default     = 345600
}

variable "visibility_timeout_seconds" {
  description = "Visibility timeout in seconds (should exceed Lambda timeout)"
  type        = number
  default     = 90
}

variable "content_based_deduplication" {
  description = "Enable content-based deduplication for FIFO queue"
  type        = bool
  default     = true
}

# --- Dead letter queue ---

variable "dlq_message_retention_seconds" {
  description = "DLQ message retention period in seconds (default: 14 days)"
  type        = number
  default     = 1209600
}

variable "max_receive_count" {
  description = "Max receive count before sending to DLQ"
  type        = number
  default     = 3
}

# --- Monitoring ---

variable "sns_topic_arn" {
  description = "SNS topic ARN for DLQ alarm notifications"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
