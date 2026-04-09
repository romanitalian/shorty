# deploy/terraform/modules/dynamodb/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- Billing mode ---

variable "links_billing_mode" {
  description = "Billing mode for links table (PAY_PER_REQUEST or PROVISIONED)"
  type        = string
  default     = "PAY_PER_REQUEST"
}

variable "clicks_billing_mode" {
  description = "Billing mode for clicks table (PAY_PER_REQUEST or PROVISIONED)"
  type        = string
  default     = "PAY_PER_REQUEST"
}

variable "users_billing_mode" {
  description = "Billing mode for users table (PAY_PER_REQUEST or PROVISIONED)"
  type        = string
  default     = "PAY_PER_REQUEST"
}

# --- Links table capacity (PROVISIONED mode only) ---

variable "links_read_capacity" {
  description = "Read capacity units for links table (PROVISIONED mode)"
  type        = number
  default     = 500
}

variable "links_write_capacity" {
  description = "Write capacity units for links table (PROVISIONED mode)"
  type        = number
  default     = 200
}

variable "links_gsi_read_capacity" {
  description = "Read capacity units for links GSI (PROVISIONED mode)"
  type        = number
  default     = 100
}

variable "links_gsi_write_capacity" {
  description = "Write capacity units for links GSI (PROVISIONED mode)"
  type        = number
  default     = 200
}

# --- Clicks table capacity (PROVISIONED mode only) ---

variable "clicks_read_capacity" {
  description = "Read capacity units for clicks table (PROVISIONED mode)"
  type        = number
  default     = 100
}

variable "clicks_write_capacity" {
  description = "Write capacity units for clicks table (PROVISIONED mode)"
  type        = number
  default     = 10000
}

variable "clicks_gsi_read_capacity" {
  description = "Read capacity units for clicks GSI (PROVISIONED mode)"
  type        = number
  default     = 100
}

variable "clicks_gsi_write_capacity" {
  description = "Write capacity units for clicks GSI (PROVISIONED mode)"
  type        = number
  default     = 10000
}

# --- Auto-scaling configuration ---

variable "enable_autoscaling" {
  description = "Enable auto-scaling for PROVISIONED tables"
  type        = bool
  default     = false
}

variable "autoscaling_target_utilization" {
  description = "Target utilization percentage for auto-scaling (0-100)"
  type        = number
  default     = 70
}

# --- Clicks table streams ---

variable "clicks_stream_enabled" {
  description = "Enable DynamoDB Streams on clicks table"
  type        = bool
  default     = true
}

# --- Deletion protection ---

variable "deletion_protection_enabled" {
  description = "Enable deletion protection on tables"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
