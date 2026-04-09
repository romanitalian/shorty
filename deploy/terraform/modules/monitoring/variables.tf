# deploy/terraform/modules/monitoring/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- Resource references ---

variable "api_gateway_id" {
  description = "API Gateway v2 API ID for metrics"
  type        = string
}

variable "redirect_function_name" {
  description = "Name of the redirect Lambda function"
  type        = string
}

variable "api_function_name" {
  description = "Name of the API Lambda function"
  type        = string
}

variable "worker_function_name" {
  description = "Name of the worker Lambda function"
  type        = string
}

variable "links_table_name" {
  description = "Name of the links DynamoDB table"
  type        = string
}

variable "clicks_table_name" {
  description = "Name of the clicks DynamoDB table"
  type        = string
}

# --- Alarm thresholds ---

variable "apigw_5xx_threshold_percent" {
  description = "API Gateway 5XX error rate threshold percentage"
  type        = number
  default     = 1
}

variable "apigw_latency_p99_threshold_ms" {
  description = "API Gateway p99 latency threshold in milliseconds"
  type        = number
  default     = 200
}

variable "lambda_error_threshold" {
  description = "Lambda error count threshold per 5 minutes"
  type        = number
  default     = 5
}

variable "lambda_cold_start_threshold_ms" {
  description = "Lambda cold start duration threshold in milliseconds"
  type        = number
  default     = 500
}

variable "dynamodb_error_threshold" {
  description = "DynamoDB system error threshold per 5 minutes"
  type        = number
  default     = 1
}

variable "dynamodb_throttle_threshold" {
  description = "DynamoDB throttle threshold per 5 minutes"
  type        = number
  default     = 10
}

# --- SNS ---

variable "sns_email" {
  description = "Email address for SNS alarm notifications"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
