# deploy/terraform/modules/lambda/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

variable "region" {
  description = "AWS region"
  type        = string
}

# --- Lambda artifact configuration ---

variable "artifacts_bucket" {
  description = "S3 bucket containing Lambda deployment packages"
  type        = string
}

variable "redirect_s3_key" {
  description = "S3 key for redirect Lambda zip"
  type        = string
  default     = "redirect.zip"
}

variable "api_s3_key" {
  description = "S3 key for api Lambda zip"
  type        = string
  default     = "api.zip"
}

variable "worker_s3_key" {
  description = "S3 key for worker Lambda zip"
  type        = string
  default     = "worker.zip"
}

# --- Memory and timeout configuration ---

variable "redirect_memory_size" {
  description = "Memory size in MB for redirect Lambda"
  type        = number
  default     = 512
}

variable "redirect_timeout" {
  description = "Timeout in seconds for redirect Lambda"
  type        = number
  default     = 5
}

variable "api_memory_size" {
  description = "Memory size in MB for api Lambda"
  type        = number
  default     = 256
}

variable "api_timeout" {
  description = "Timeout in seconds for api Lambda"
  type        = number
  default     = 10
}

variable "worker_memory_size" {
  description = "Memory size in MB for worker Lambda"
  type        = number
  default     = 128
}

variable "worker_timeout" {
  description = "Timeout in seconds for worker Lambda"
  type        = number
  default     = 60
}

# --- Concurrency configuration ---

variable "redirect_provisioned_concurrency" {
  description = "Provisioned concurrency for redirect Lambda (attached to live alias)"
  type        = number
  default     = 2
}

variable "redirect_reserved_concurrency" {
  description = "Reserved concurrency for redirect Lambda"
  type        = number
  default     = 1000
}

variable "api_reserved_concurrency" {
  description = "Reserved concurrency for api Lambda"
  type        = number
  default     = 200
}

variable "worker_reserved_concurrency" {
  description = "Reserved concurrency for worker Lambda"
  type        = number
  default     = 50
}

# --- VPC configuration ---

variable "subnet_ids" {
  description = "Private subnet IDs for Lambda VPC configuration"
  type        = list(string)
}

variable "security_group_ids" {
  description = "Security group IDs for Lambda VPC configuration"
  type        = list(string)
}

# --- DynamoDB table ARNs (for IAM policies) ---

variable "dynamodb_links_table_arn" {
  description = "ARN of the links DynamoDB table"
  type        = string
}

variable "dynamodb_clicks_table_arn" {
  description = "ARN of the clicks DynamoDB table"
  type        = string
}

variable "dynamodb_users_table_arn" {
  description = "ARN of the users DynamoDB table"
  type        = string
}

# --- SQS ARNs (for IAM policies) ---

variable "sqs_clicks_queue_arn" {
  description = "ARN of the clicks SQS FIFO queue"
  type        = string
}

# --- Environment variables ---

variable "redirect_environment_variables" {
  description = "Environment variables for redirect Lambda"
  type        = map(string)
  default     = {}
}

variable "api_environment_variables" {
  description = "Environment variables for api Lambda"
  type        = map(string)
  default     = {}
}

variable "worker_environment_variables" {
  description = "Environment variables for worker Lambda"
  type        = map(string)
  default     = {}
}

# --- Log retention ---

variable "log_retention_days" {
  description = "CloudWatch log retention in days (14 for dev, 90 for prod)"
  type        = number
  default     = 14
}

# --- SQS event source mapping for worker ---

variable "sqs_clicks_queue_arn_for_trigger" {
  description = "SQS queue ARN used as event source for the worker Lambda"
  type        = string
}

variable "worker_dlq_arn" {
  description = "ARN of the dead letter queue for the worker Lambda"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
