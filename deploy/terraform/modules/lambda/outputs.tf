# deploy/terraform/modules/lambda/outputs.tf

# --- Redirect Lambda ---

output "redirect_function_name" {
  description = "Name of the redirect Lambda function"
  value       = aws_lambda_function.redirect.function_name
}

output "redirect_function_arn" {
  description = "ARN of the redirect Lambda function"
  value       = aws_lambda_function.redirect.arn
}

output "redirect_invoke_arn" {
  description = "Invoke ARN of the redirect Lambda function"
  value       = aws_lambda_function.redirect.invoke_arn
}

output "redirect_alias_arn" {
  description = "ARN of the redirect Lambda live alias"
  value       = aws_lambda_alias.redirect_live.arn
}

output "redirect_alias_invoke_arn" {
  description = "Invoke ARN of the redirect Lambda live alias"
  value       = aws_lambda_alias.redirect_live.invoke_arn
}

output "redirect_role_arn" {
  description = "ARN of the redirect Lambda IAM role"
  value       = aws_iam_role.redirect.arn
}

# --- API Lambda ---

output "api_function_name" {
  description = "Name of the API Lambda function"
  value       = aws_lambda_function.api.function_name
}

output "api_function_arn" {
  description = "ARN of the API Lambda function"
  value       = aws_lambda_function.api.arn
}

output "api_invoke_arn" {
  description = "Invoke ARN of the API Lambda function"
  value       = aws_lambda_function.api.invoke_arn
}

output "api_alias_arn" {
  description = "ARN of the API Lambda live alias"
  value       = aws_lambda_alias.api_live.arn
}

output "api_alias_invoke_arn" {
  description = "Invoke ARN of the API Lambda live alias"
  value       = aws_lambda_alias.api_live.invoke_arn
}

output "api_role_arn" {
  description = "ARN of the API Lambda IAM role"
  value       = aws_iam_role.api.arn
}

# --- Worker Lambda ---

output "worker_function_name" {
  description = "Name of the worker Lambda function"
  value       = aws_lambda_function.worker.function_name
}

output "worker_function_arn" {
  description = "ARN of the worker Lambda function"
  value       = aws_lambda_function.worker.arn
}

output "worker_invoke_arn" {
  description = "Invoke ARN of the worker Lambda function"
  value       = aws_lambda_function.worker.invoke_arn
}

output "worker_alias_arn" {
  description = "ARN of the worker Lambda live alias"
  value       = aws_lambda_alias.worker_live.arn
}

output "worker_role_arn" {
  description = "ARN of the worker Lambda IAM role"
  value       = aws_iam_role.worker.arn
}
