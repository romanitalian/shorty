# deploy/terraform/modules/elasticache/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- Redis configuration ---

variable "node_type" {
  description = "ElastiCache node type (e.g., cache.t4g.small for dev, cache.r7g.large for prod)"
  type        = string
  default     = "cache.t4g.small"
}

variable "num_cache_clusters" {
  description = "Number of cache clusters (1 for dev, 2+ for prod with Multi-AZ)"
  type        = number
  default     = 1
}

variable "engine_version" {
  description = "Redis engine version"
  type        = string
  default     = "7.1"
}

variable "port" {
  description = "Redis port"
  type        = number
  default     = 6379
}

variable "automatic_failover_enabled" {
  description = "Enable automatic failover (requires num_cache_clusters >= 2)"
  type        = bool
  default     = false
}

variable "multi_az_enabled" {
  description = "Enable Multi-AZ (requires automatic_failover_enabled)"
  type        = bool
  default     = false
}

variable "at_rest_encryption_enabled" {
  description = "Enable encryption at rest"
  type        = bool
  default     = true
}

variable "transit_encryption_enabled" {
  description = "Enable encryption in transit"
  type        = bool
  default     = true
}

variable "auth_token" {
  description = "Redis AUTH token for transit encryption"
  type        = string
  default     = ""
  sensitive   = true
}

variable "snapshot_retention_limit" {
  description = "Number of days to retain automatic snapshots (0 to disable)"
  type        = number
  default     = 0
}

# --- Network ---

variable "vpc_id" {
  description = "VPC ID for ElastiCache"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for ElastiCache subnet group"
  type        = list(string)
}

variable "lambda_security_group_id" {
  description = "Security group ID of Lambda functions (allowed to connect to Redis)"
  type        = string
}

# --- Maintenance ---

variable "maintenance_window" {
  description = "Weekly maintenance window"
  type        = string
  default     = "sun:05:00-sun:06:00"
}

variable "snapshot_window" {
  description = "Daily snapshot window"
  type        = string
  default     = "03:00-04:00"
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
