# deploy/terraform/modules/cognito/outputs.tf

output "user_pool_id" {
  description = "ID of the Cognito User Pool"
  value       = aws_cognito_user_pool.this.id
}

output "user_pool_arn" {
  description = "ARN of the Cognito User Pool"
  value       = aws_cognito_user_pool.this.arn
}

output "user_pool_endpoint" {
  description = "Endpoint of the Cognito User Pool"
  value       = aws_cognito_user_pool.this.endpoint
}

output "client_id" {
  description = "ID of the Cognito App Client"
  value       = aws_cognito_user_pool_client.this.id
}

output "domain" {
  description = "Cognito hosted UI domain"
  value       = var.custom_domain != "" ? var.custom_domain : (var.domain_prefix != "" ? "${var.domain_prefix}.auth.${data.aws_region.current.name}.amazoncognito.com" : "")
}

data "aws_region" "current" {}
