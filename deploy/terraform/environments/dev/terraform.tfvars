# deploy/terraform/environments/dev/terraform.tfvars
#
# Dev environment variable values.
# Sensitive values (vpc_id, subnet_ids, sg_id) should be set via
# environment variables (TF_VAR_*) or a .auto.tfvars file NOT checked in.

# These are placeholders — replace with actual AWS resource IDs.
vpc_id                   = "vpc-dev-placeholder"
private_subnet_ids       = ["subnet-dev-a", "subnet-dev-b"]
lambda_security_group_id = "sg-dev-lambda-placeholder"

alert_email = ""

cognito_callback_urls = ["http://localhost:8080/auth/callback"]
cognito_logout_urls   = ["http://localhost:8080/"]
