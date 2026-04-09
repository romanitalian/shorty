# deploy/terraform/modules/waf/outputs.tf

output "web_acl_id" {
  description = "ID of the WAF Web ACL"
  value       = aws_wafv2_web_acl.this.id
}

output "web_acl_arn" {
  description = "ARN of the WAF Web ACL"
  value       = aws_wafv2_web_acl.this.arn
}

output "blocked_ips_v4_id" {
  description = "ID of the IPv4 blocklist IP set"
  value       = aws_wafv2_ip_set.blocked_ips_v4.id
}

output "blocked_ips_v6_id" {
  description = "ID of the IPv6 blocklist IP set"
  value       = aws_wafv2_ip_set.blocked_ips_v6.id
}

output "waf_log_group_arn" {
  description = "ARN of the WAF CloudWatch log group"
  value       = aws_cloudwatch_log_group.waf.arn
}
