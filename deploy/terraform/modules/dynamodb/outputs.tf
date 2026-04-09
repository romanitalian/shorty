# deploy/terraform/modules/dynamodb/outputs.tf

# --- Links table ---

output "links_table_name" {
  description = "Name of the links DynamoDB table"
  value       = aws_dynamodb_table.links.name
}

output "links_table_arn" {
  description = "ARN of the links DynamoDB table"
  value       = aws_dynamodb_table.links.arn
}

output "links_table_id" {
  description = "ID of the links DynamoDB table"
  value       = aws_dynamodb_table.links.id
}

# --- Clicks table ---

output "clicks_table_name" {
  description = "Name of the clicks DynamoDB table"
  value       = aws_dynamodb_table.clicks.name
}

output "clicks_table_arn" {
  description = "ARN of the clicks DynamoDB table"
  value       = aws_dynamodb_table.clicks.arn
}

output "clicks_table_id" {
  description = "ID of the clicks DynamoDB table"
  value       = aws_dynamodb_table.clicks.id
}

output "clicks_stream_arn" {
  description = "ARN of the clicks DynamoDB Stream"
  value       = var.clicks_stream_enabled ? aws_dynamodb_table.clicks.stream_arn : null
}

# --- Users table ---

output "users_table_name" {
  description = "Name of the users DynamoDB table"
  value       = aws_dynamodb_table.users.name
}

output "users_table_arn" {
  description = "ARN of the users DynamoDB table"
  value       = aws_dynamodb_table.users.arn
}

output "users_table_id" {
  description = "ID of the users DynamoDB table"
  value       = aws_dynamodb_table.users.id
}
