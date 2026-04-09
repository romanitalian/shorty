# deploy/terraform/modules/api_gateway/outputs.tf

output "api_id" {
  description = "ID of the HTTP API"
  value       = aws_apigatewayv2_api.this.id
}

output "api_endpoint" {
  description = "Default endpoint of the HTTP API"
  value       = aws_apigatewayv2_api.this.api_endpoint
}

output "api_execution_arn" {
  description = "Execution ARN of the HTTP API"
  value       = aws_apigatewayv2_api.this.execution_arn
}

output "stage_id" {
  description = "ID of the default stage"
  value       = aws_apigatewayv2_stage.default.id
}

output "stage_invoke_url" {
  description = "Invoke URL of the default stage"
  value       = aws_apigatewayv2_stage.default.invoke_url
}

output "custom_domain_target" {
  description = "Target domain name for DNS (when custom domain is enabled)"
  value       = var.enable_custom_domain ? aws_apigatewayv2_domain_name.this[0].domain_name_configuration[0].target_domain_name : null
}

output "custom_domain_zone_id" {
  description = "Hosted zone ID for custom domain (when enabled)"
  value       = var.enable_custom_domain ? aws_apigatewayv2_domain_name.this[0].domain_name_configuration[0].hosted_zone_id : null
}
