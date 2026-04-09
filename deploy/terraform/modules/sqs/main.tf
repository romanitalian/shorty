# deploy/terraform/modules/sqs/main.tf

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

  queue_name = "${var.project}-${var.queue_name}.fifo"
  dlq_name   = "${var.project}-${var.queue_name}-dlq.fifo"
}

# =============================================================================
# Dead Letter Queue
# =============================================================================

resource "aws_sqs_queue" "dlq" {
  name                        = local.dlq_name
  fifo_queue                  = true
  content_based_deduplication = true
  message_retention_seconds   = var.dlq_message_retention_seconds

  tags = merge(local.common_tags, {
    component = "dlq"
  })
}

# =============================================================================
# Main FIFO Queue
# =============================================================================

resource "aws_sqs_queue" "this" {
  name                        = local.queue_name
  fifo_queue                  = true
  content_based_deduplication = var.content_based_deduplication
  message_retention_seconds   = var.message_retention_seconds
  visibility_timeout_seconds  = var.visibility_timeout_seconds

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = var.max_receive_count
  })

  tags = merge(local.common_tags, {
    component = "clicks-queue"
  })
}

# =============================================================================
# DLQ Redrive Allow Policy
# =============================================================================

resource "aws_sqs_queue_redrive_allow_policy" "dlq" {
  queue_url = aws_sqs_queue.dlq.id

  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue"
    sourceQueueArns   = [aws_sqs_queue.this.arn]
  })
}

# =============================================================================
# CloudWatch Alarm: DLQ Messages Visible
# =============================================================================

resource "aws_cloudwatch_metric_alarm" "dlq_messages" {
  count               = var.sns_topic_arn != "" ? 1 : 0
  alarm_name          = "${var.project}-${var.queue_name}-dlq-not-empty"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Maximum"
  threshold           = 0
  alarm_description   = "Dead letter queue has messages -- failed click events need investigation"
  alarm_actions       = [var.sns_topic_arn]

  dimensions = {
    QueueName = local.dlq_name
  }

  tags = local.common_tags
}
