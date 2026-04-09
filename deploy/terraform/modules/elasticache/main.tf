# deploy/terraform/modules/elasticache/main.tf

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

  replication_group_id = "${var.project}-${var.environment}-redis"
}

# =============================================================================
# Subnet Group
# =============================================================================

resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.project}-${var.environment}-redis"
  subnet_ids = var.subnet_ids

  tags = local.common_tags
}

# =============================================================================
# Parameter Group
# =============================================================================

resource "aws_elasticache_parameter_group" "this" {
  name   = "${var.project}-${var.environment}-redis7"
  family = "redis7"

  parameter {
    name  = "maxmemory-policy"
    value = "allkeys-lfu"
  }

  tags = local.common_tags
}

# =============================================================================
# Security Group
# =============================================================================

resource "aws_security_group" "redis" {
  name_prefix = "${var.project}-${var.environment}-redis-"
  description = "Security group for ${var.project} ElastiCache Redis"
  vpc_id      = var.vpc_id

  # Ingress: only from Lambda security group
  ingress {
    description     = "Redis from Lambda"
    from_port       = var.port
    to_port         = var.port
    protocol        = "tcp"
    security_groups = [var.lambda_security_group_id]
  }

  # No egress needed
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = []
  }

  tags = merge(local.common_tags, {
    Name = "${var.project}-${var.environment}-redis"
  })

  lifecycle {
    create_before_destroy = true
  }
}

# =============================================================================
# Redis Replication Group
# =============================================================================

resource "aws_elasticache_replication_group" "this" {
  replication_group_id = local.replication_group_id
  description          = "${var.project} Redis cache (${var.environment})"

  node_type            = var.node_type
  num_cache_clusters   = var.num_cache_clusters
  port                 = var.port
  engine_version       = var.engine_version
  parameter_group_name = aws_elasticache_parameter_group.this.name
  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.redis.id]

  automatic_failover_enabled = var.automatic_failover_enabled
  multi_az_enabled           = var.multi_az_enabled

  at_rest_encryption_enabled = var.at_rest_encryption_enabled
  transit_encryption_enabled = var.transit_encryption_enabled
  auth_token                 = var.transit_encryption_enabled && var.auth_token != "" ? var.auth_token : null

  maintenance_window       = var.maintenance_window
  snapshot_window          = var.snapshot_retention_limit > 0 ? var.snapshot_window : null
  snapshot_retention_limit = var.snapshot_retention_limit

  apply_immediately = var.environment != "prod"

  tags = local.common_tags
}
