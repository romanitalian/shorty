# deploy/terraform/modules/dynamodb/main.tf

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

locals {
  common_tags = merge({
    project     = var.project
    environment = var.environment
    managed_by  = "terraform"
  }, var.tags)

  links_provisioned  = var.links_billing_mode == "PROVISIONED"
  clicks_provisioned = var.clicks_billing_mode == "PROVISIONED"
}

# =============================================================================
# Links Table
# =============================================================================

resource "aws_dynamodb_table" "links" {
  name         = "${var.project}-links"
  billing_mode = var.links_billing_mode
  hash_key     = "PK"
  range_key    = "SK"

  read_capacity  = local.links_provisioned ? var.links_read_capacity : null
  write_capacity = local.links_provisioned ? var.links_write_capacity : null

  deletion_protection_enabled = var.deletion_protection_enabled

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  attribute {
    name = "owner_id"
    type = "S"
  }

  attribute {
    name = "created_at"
    type = "N"
  }

  global_secondary_index {
    name            = "owner_id-created_at-index"
    hash_key        = "owner_id"
    range_key       = "created_at"
    projection_type = "ALL"

    read_capacity  = local.links_provisioned ? var.links_gsi_read_capacity : null
    write_capacity = local.links_provisioned ? var.links_gsi_write_capacity : null
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  server_side_encryption {
    enabled = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(local.common_tags, {
    table = "links"
  })
}

# --- Links table auto-scaling ---

resource "aws_appautoscaling_target" "links_read" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 2500
  min_capacity       = var.links_read_capacity
  resource_id        = "table/${aws_dynamodb_table.links.name}"
  scalable_dimension = "dynamodb:table:ReadCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "links_read" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-links-read-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.links_read[0].resource_id
  scalable_dimension = aws_appautoscaling_target.links_read[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.links_read[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_target" "links_write" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 1200
  min_capacity       = var.links_write_capacity
  resource_id        = "table/${aws_dynamodb_table.links.name}"
  scalable_dimension = "dynamodb:table:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "links_write" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-links-write-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.links_write[0].resource_id
  scalable_dimension = aws_appautoscaling_target.links_write[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.links_write[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

# --- Links GSI auto-scaling ---

resource "aws_appautoscaling_target" "links_gsi_read" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 2500
  min_capacity       = var.links_gsi_read_capacity
  resource_id        = "table/${aws_dynamodb_table.links.name}/index/owner_id-created_at-index"
  scalable_dimension = "dynamodb:index:ReadCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "links_gsi_read" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-links-gsi-read-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.links_gsi_read[0].resource_id
  scalable_dimension = aws_appautoscaling_target.links_gsi_read[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.links_gsi_read[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_target" "links_gsi_write" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 1200
  min_capacity       = var.links_gsi_write_capacity
  resource_id        = "table/${aws_dynamodb_table.links.name}/index/owner_id-created_at-index"
  scalable_dimension = "dynamodb:index:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "links_gsi_write" {
  count              = local.links_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-links-gsi-write-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.links_gsi_write[0].resource_id
  scalable_dimension = aws_appautoscaling_target.links_gsi_write[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.links_gsi_write[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

# =============================================================================
# Clicks Table
# =============================================================================

resource "aws_dynamodb_table" "clicks" {
  name         = "${var.project}-clicks"
  billing_mode = var.clicks_billing_mode
  hash_key     = "PK"
  range_key    = "SK"

  read_capacity  = local.clicks_provisioned ? var.clicks_read_capacity : null
  write_capacity = local.clicks_provisioned ? var.clicks_write_capacity : null

  deletion_protection_enabled = var.deletion_protection_enabled

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  attribute {
    name = "created_at"
    type = "N"
  }

  global_secondary_index {
    name            = "code-date-index"
    hash_key        = "PK"
    range_key       = "created_at"
    projection_type = "INCLUDE"
    non_key_attributes = [
      "country",
      "device_type",
      "referer_domain",
      "ip_hash",
    ]

    read_capacity  = local.clicks_provisioned ? var.clicks_gsi_read_capacity : null
    write_capacity = local.clicks_provisioned ? var.clicks_gsi_write_capacity : null
  }

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  server_side_encryption {
    enabled = true
  }

  point_in_time_recovery {
    enabled = true
  }

  stream_enabled   = var.clicks_stream_enabled
  stream_view_type = var.clicks_stream_enabled ? "NEW_IMAGE" : null

  tags = merge(local.common_tags, {
    table = "clicks"
  })
}

# --- Clicks table auto-scaling ---

resource "aws_appautoscaling_target" "clicks_write" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 25000
  min_capacity       = var.clicks_write_capacity
  resource_id        = "table/${aws_dynamodb_table.clicks.name}"
  scalable_dimension = "dynamodb:table:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_write" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-clicks-write-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_write[0].resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_write[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_write[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_target" "clicks_read" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 500
  min_capacity       = var.clicks_read_capacity
  resource_id        = "table/${aws_dynamodb_table.clicks.name}"
  scalable_dimension = "dynamodb:table:ReadCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_read" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-clicks-read-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_read[0].resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_read[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_read[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

# --- Clicks GSI auto-scaling ---

resource "aws_appautoscaling_target" "clicks_gsi_write" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 25000
  min_capacity       = var.clicks_gsi_write_capacity
  resource_id        = "table/${aws_dynamodb_table.clicks.name}/index/code-date-index"
  scalable_dimension = "dynamodb:index:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_gsi_write" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-clicks-gsi-write-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_gsi_write[0].resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_gsi_write[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_gsi_write[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_target" "clicks_gsi_read" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  max_capacity       = 500
  min_capacity       = var.clicks_gsi_read_capacity
  resource_id        = "table/${aws_dynamodb_table.clicks.name}/index/code-date-index"
  scalable_dimension = "dynamodb:index:ReadCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_gsi_read" {
  count              = local.clicks_provisioned && var.enable_autoscaling ? 1 : 0
  name               = "${var.project}-clicks-gsi-read-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_gsi_read[0].resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_gsi_read[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_gsi_read[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }
    target_value       = var.autoscaling_target_utilization
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

# =============================================================================
# Users Table
# =============================================================================

resource "aws_dynamodb_table" "users" {
  name         = "${var.project}-users"
  billing_mode = var.users_billing_mode
  hash_key     = "PK"
  range_key    = "SK"

  deletion_protection_enabled = var.deletion_protection_enabled

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  server_side_encryption {
    enabled = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(local.common_tags, {
    table = "users"
  })
}
