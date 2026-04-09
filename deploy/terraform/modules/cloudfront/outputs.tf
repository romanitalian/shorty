# deploy/terraform/modules/cloudfront/outputs.tf

output "distribution_id" {
  description = "ID of the CloudFront distribution"
  value       = aws_cloudfront_distribution.this.id
}

output "distribution_arn" {
  description = "ARN of the CloudFront distribution"
  value       = aws_cloudfront_distribution.this.arn
}

output "distribution_domain_name" {
  description = "Domain name of the CloudFront distribution"
  value       = aws_cloudfront_distribution.this.domain_name
}

output "distribution_hosted_zone_id" {
  description = "Route 53 zone ID of the CloudFront distribution"
  value       = aws_cloudfront_distribution.this.hosted_zone_id
}

output "response_headers_policy_id" {
  description = "ID of the security response headers policy"
  value       = aws_cloudfront_response_headers_policy.security.id
}

output "cache_policy_no_cache_id" {
  description = "ID of the no-cache policy"
  value       = aws_cloudfront_cache_policy.no_cache.id
}
