# deploy/terraform/modules/elasticache/outputs.tf

output "replication_group_id" {
  description = "ID of the Redis replication group"
  value       = aws_elasticache_replication_group.this.id
}

output "primary_endpoint_address" {
  description = "Primary endpoint address of the Redis replication group"
  value       = aws_elasticache_replication_group.this.primary_endpoint_address
}

output "reader_endpoint_address" {
  description = "Reader endpoint address of the Redis replication group"
  value       = aws_elasticache_replication_group.this.reader_endpoint_address
}

output "port" {
  description = "Redis port"
  value       = var.port
}

output "security_group_id" {
  description = "ID of the Redis security group"
  value       = aws_security_group.redis.id
}

output "redis_address" {
  description = "Full Redis address (host:port)"
  value       = "${aws_elasticache_replication_group.this.primary_endpoint_address}:${var.port}"
}
