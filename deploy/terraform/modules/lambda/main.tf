# deploy/terraform/modules/lambda/main.tf

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

data "aws_caller_identity" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id

  common_tags = merge({
    project     = var.project
    environment = var.environment
    managed_by  = "terraform"
  }, var.tags)

  redirect_function_name = "${var.project}-${var.environment}-redirect"
  api_function_name      = "${var.project}-${var.environment}-api"
  worker_function_name   = "${var.project}-${var.environment}-worker"
}

# =============================================================================
# IAM Trust Policy (shared by all Lambda roles)
# =============================================================================

data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

# =============================================================================
# Redirect Lambda
# =============================================================================

resource "aws_cloudwatch_log_group" "redirect" {
  name              = "/aws/lambda/${local.redirect_function_name}"
  retention_in_days = var.log_retention_days

  tags = merge(local.common_tags, {
    component = "redirect"
  })
}

resource "aws_iam_role" "redirect" {
  name               = "${var.project}-${var.environment}-redirect-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = merge(local.common_tags, {
    component = "redirect"
  })
}

data "aws_iam_policy_document" "redirect" {
  # DynamoDB: read-only on links table
  statement {
    sid       = "DynamoDBReadLinks"
    effect    = "Allow"
    actions   = ["dynamodb:GetItem"]
    resources = [var.dynamodb_links_table_arn]
  }

  # SQS: send click events
  statement {
    sid       = "SQSSendClickEvents"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [var.sqs_clicks_queue_arn]
  }

  # Secrets Manager: IP salt and Redis auth
  statement {
    sid     = "SecretsManagerAccess"
    effect  = "Allow"
    actions = ["secretsmanager:GetSecretValue"]
    resources = [
      "arn:aws:secretsmanager:${var.region}:${local.account_id}:secret:${var.project}/ip-salt-*",
      "arn:aws:secretsmanager:${var.region}:${local.account_id}:secret:${var.project}/redis-auth-*",
    ]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["${aws_cloudwatch_log_group.redirect.arn}:*"]
  }

  # X-Ray
  statement {
    sid    = "XRayTracing"
    effect = "Allow"
    actions = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords",
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    sid       = "CloudWatchMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }

  # VPC ENI management
  statement {
    sid    = "VPCNetworkInterfaces"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "redirect" {
  name   = "${var.project}-${var.environment}-redirect-lambda-policy"
  role   = aws_iam_role.redirect.id
  policy = data.aws_iam_policy_document.redirect.json
}

resource "aws_lambda_function" "redirect" {
  function_name = local.redirect_function_name
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = var.redirect_memory_size
  timeout       = var.redirect_timeout
  role          = aws_iam_role.redirect.arn

  s3_bucket = var.artifacts_bucket
  s3_key    = var.redirect_s3_key

  reserved_concurrent_executions = var.redirect_reserved_concurrency

  vpc_config {
    subnet_ids         = var.subnet_ids
    security_group_ids = var.security_group_ids
  }

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = merge({
      GOMAXPROCS = "1"
    }, var.redirect_environment_variables)
  }

  depends_on = [
    aws_cloudwatch_log_group.redirect,
    aws_iam_role_policy.redirect,
  ]

  tags = merge(local.common_tags, {
    component = "redirect"
  })
}

resource "aws_lambda_alias" "redirect_live" {
  name             = "live"
  function_name    = aws_lambda_function.redirect.function_name
  function_version = aws_lambda_function.redirect.version
}

resource "aws_lambda_provisioned_concurrency_config" "redirect" {
  count = var.redirect_provisioned_concurrency > 0 ? 1 : 0

  function_name                  = aws_lambda_function.redirect.function_name
  provisioned_concurrent_executions = var.redirect_provisioned_concurrency
  qualifier                      = aws_lambda_alias.redirect_live.name
}

resource "aws_lambda_function_event_invoke_config" "redirect" {
  function_name          = aws_lambda_function.redirect.function_name
  maximum_retry_attempts = 0
}

# =============================================================================
# API Lambda
# =============================================================================

resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/lambda/${local.api_function_name}"
  retention_in_days = var.log_retention_days

  tags = merge(local.common_tags, {
    component = "api"
  })
}

resource "aws_iam_role" "api" {
  name               = "${var.project}-${var.environment}-api-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = merge(local.common_tags, {
    component = "api"
  })
}

data "aws_iam_policy_document" "api" {
  # DynamoDB: full CRUD on links table
  statement {
    sid    = "DynamoDBCRUDLinks"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
    ]
    resources = [var.dynamodb_links_table_arn]
  }

  # DynamoDB: query links GSI (user dashboard)
  statement {
    sid       = "DynamoDBQueryLinksGSI"
    effect    = "Allow"
    actions   = ["dynamodb:Query"]
    resources = ["${var.dynamodb_links_table_arn}/index/owner_id-created_at-index"]
  }

  # DynamoDB: query clicks GSI (stats)
  statement {
    sid       = "DynamoDBQueryClicksGSI"
    effect    = "Allow"
    actions   = ["dynamodb:Query"]
    resources = ["${var.dynamodb_clicks_table_arn}/index/code-date-index"]
  }

  # DynamoDB: users table
  statement {
    sid    = "DynamoDBUsersTable"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
    ]
    resources = [var.dynamodb_users_table_arn]
  }

  # Secrets Manager: IP salt + JWT keys + Redis auth
  statement {
    sid     = "SecretsManagerAccess"
    effect  = "Allow"
    actions = ["secretsmanager:GetSecretValue"]
    resources = [
      "arn:aws:secretsmanager:${var.region}:${local.account_id}:secret:${var.project}/ip-salt-*",
      "arn:aws:secretsmanager:${var.region}:${local.account_id}:secret:${var.project}/jwt-*",
      "arn:aws:secretsmanager:${var.region}:${local.account_id}:secret:${var.project}/redis-auth-*",
    ]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["${aws_cloudwatch_log_group.api.arn}:*"]
  }

  # X-Ray
  statement {
    sid    = "XRayTracing"
    effect = "Allow"
    actions = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords",
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    sid       = "CloudWatchMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }

  # VPC ENI management
  statement {
    sid    = "VPCNetworkInterfaces"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "api" {
  name   = "${var.project}-${var.environment}-api-lambda-policy"
  role   = aws_iam_role.api.id
  policy = data.aws_iam_policy_document.api.json
}

resource "aws_lambda_function" "api" {
  function_name = local.api_function_name
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = var.api_memory_size
  timeout       = var.api_timeout
  role          = aws_iam_role.api.arn

  s3_bucket = var.artifacts_bucket
  s3_key    = var.api_s3_key

  reserved_concurrent_executions = var.api_reserved_concurrency

  vpc_config {
    subnet_ids         = var.subnet_ids
    security_group_ids = var.security_group_ids
  }

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = merge({
      GOMAXPROCS = "1"
    }, var.api_environment_variables)
  }

  depends_on = [
    aws_cloudwatch_log_group.api,
    aws_iam_role_policy.api,
  ]

  tags = merge(local.common_tags, {
    component = "api"
  })
}

resource "aws_lambda_alias" "api_live" {
  name             = "live"
  function_name    = aws_lambda_function.api.function_name
  function_version = aws_lambda_function.api.version
}

# =============================================================================
# Worker Lambda
# =============================================================================

resource "aws_cloudwatch_log_group" "worker" {
  name              = "/aws/lambda/${local.worker_function_name}"
  retention_in_days = var.log_retention_days

  tags = merge(local.common_tags, {
    component = "worker"
  })
}

resource "aws_iam_role" "worker" {
  name               = "${var.project}-${var.environment}-worker-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = merge(local.common_tags, {
    component = "worker"
  })
}

data "aws_iam_policy_document" "worker" {
  # SQS: consume click events
  statement {
    sid    = "SQSConsumeClickEvents"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = [var.sqs_clicks_queue_arn]
  }

  # DynamoDB: batch write to clicks table
  statement {
    sid       = "DynamoDBBatchWriteClicks"
    effect    = "Allow"
    actions   = ["dynamodb:BatchWriteItem"]
    resources = [var.dynamodb_clicks_table_arn]
  }

  # DynamoDB: update click_count on links table
  statement {
    sid       = "DynamoDBUpdateLinkClickCount"
    effect    = "Allow"
    actions   = ["dynamodb:UpdateItem"]
    resources = [var.dynamodb_links_table_arn]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["${aws_cloudwatch_log_group.worker.arn}:*"]
  }

  # X-Ray
  statement {
    sid    = "XRayTracing"
    effect = "Allow"
    actions = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords",
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    sid       = "CloudWatchMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "worker" {
  name   = "${var.project}-${var.environment}-worker-lambda-policy"
  role   = aws_iam_role.worker.id
  policy = data.aws_iam_policy_document.worker.json
}

resource "aws_lambda_function" "worker" {
  function_name = local.worker_function_name
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = var.worker_memory_size
  timeout       = var.worker_timeout
  role          = aws_iam_role.worker.arn

  s3_bucket = var.artifacts_bucket
  s3_key    = var.worker_s3_key

  reserved_concurrent_executions = var.worker_reserved_concurrency

  dead_letter_config {
    target_arn = var.worker_dlq_arn
  }

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = merge({
      GOMAXPROCS = "1"
    }, var.worker_environment_variables)
  }

  depends_on = [
    aws_cloudwatch_log_group.worker,
    aws_iam_role_policy.worker,
  ]

  tags = merge(local.common_tags, {
    component = "worker"
  })
}

resource "aws_lambda_alias" "worker_live" {
  name             = "live"
  function_name    = aws_lambda_function.worker.function_name
  function_version = aws_lambda_function.worker.version
}

# =============================================================================
# SQS Event Source Mapping for Worker
# =============================================================================

resource "aws_lambda_event_source_mapping" "worker_sqs" {
  event_source_arn                   = var.sqs_clicks_queue_arn_for_trigger
  function_name                      = aws_lambda_alias.worker_live.arn
  batch_size                         = 10
  maximum_batching_window_in_seconds = 5
  enabled                            = true

  scaling_config {
    maximum_concurrency = var.worker_reserved_concurrency
  }

  function_response_types = ["ReportBatchItemFailures"]
}
