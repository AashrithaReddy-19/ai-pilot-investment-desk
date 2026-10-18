terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region     = var.aws_region
  access_key = var.aws_access_key != "" ? var.aws_access_key : null
  secret_key = var.aws_secret_key != "" ? var.aws_secret_key : null

  default_tags {
    tags = {
      Project     = var.project_name
      Environment = var.environment
      ManagedBy   = "OpenTofu"
    }
  }
}

# ------------------------------------------------------------------------------
# Single-instance layout: everything runs on one EC2 host.
#
#   browser ──:80──> nginx ─┬─ /            built React app (frontend/dist)
#                           └─ /api /health /ready ──> Go gateway   127.0.0.1:app_port
#                                                        ├─> decision engine 127.0.0.1:decision_engine_port
#                                                        ├─> PostgreSQL       127.0.0.1:5432
#                                                        └─> Redis            127.0.0.1:6379
#
# Only nginx (and SSH, when allowed_ssh_cidr is set) is reachable from outside.
#
# With ai_assist on, the gateway's balance-sheet AI reader also calls Claude on
# Amazon Bedrock and Amazon Textract, as the instance's IAM role (no keys on the
# host). Textract reads a scan from the private bucket below, which deletes
# anything left in it after a day.
# ------------------------------------------------------------------------------

# ------------------------------------------------------------------------------
# Networking: Default VPC & Subnets
# ------------------------------------------------------------------------------
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# ------------------------------------------------------------------------------
# AMI: Official Canonical Ubuntu 24.04 LTS (Noble)
# ------------------------------------------------------------------------------
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# ------------------------------------------------------------------------------
# SSH Key Pair (Optional)
# ------------------------------------------------------------------------------
resource "aws_key_pair" "deployer" {
  count      = var.public_key != "" ? 1 : 0
  key_name   = "${var.project_name}-key"
  public_key = var.public_key
}

# ------------------------------------------------------------------------------
# Security Group: HTTP to nginx, SSH only when asked for. Gateway, engine,
# PostgreSQL and Redis listen on localhost and are never opened.
# ------------------------------------------------------------------------------
resource "aws_security_group" "app" {
  name        = "${var.project_name}-sg"
  description = "C23 Decision Desk single EC2 host"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTP (nginx: frontend and API)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = var.allowed_http_cidr
  }

  dynamic "ingress" {
    for_each = length(var.allowed_ssh_cidr) > 0 ? [1] : []
    content {
      description = "SSH Access"
      from_port   = 22
      to_port     = 22
      protocol    = "tcp"
      cidr_blocks = var.allowed_ssh_cidr
    }
  }

  egress {
    description = "Allow all outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.project_name}-sg"
  }
}

# ------------------------------------------------------------------------------
# EC2 Instance & Automated Deployment (user_data.sh.tftpl)
# ------------------------------------------------------------------------------
data "aws_caller_identity" "current" {}

locals {
  bedrock_region = var.bedrock_region != "" ? var.bedrock_region : var.aws_region
  scan_bucket    = var.ai_assist ? aws_s3_bucket.scans[0].bucket : ""

  user_data = templatefile("${path.module}/user_data.sh.tftpl", {
    repo_url              = var.repo_url
    git_ref               = var.git_ref
    git_token             = var.git_token
    go_version            = var.go_version
    node_major            = var.node_major
    swap_gb               = var.swap_gb
    app_port              = var.app_port
    engine_port           = var.decision_engine_port
    db_name               = var.db_name
    db_user               = var.db_user
    db_password           = var.db_password
    allowed_origins       = var.allowed_origins
    auth_mode             = var.auth_mode
    rate_limit_per_minute = var.rate_limit_per_minute
    ai_assist             = var.ai_assist
    aws_region            = var.aws_region
    bedrock_region        = local.bedrock_region
    bedrock_model         = var.bedrock_model
    scan_bucket           = local.scan_bucket
    ai_timeout_seconds    = var.ai_timeout_seconds
    textract_max_pages    = var.textract_max_pages
  })
}

resource "aws_instance" "app" {
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = var.instance_type
  subnet_id                   = data.aws_subnets.default.ids[0]
  vpc_security_group_ids      = [aws_security_group.app.id]
  associate_public_ip_address = true
  key_name                    = var.public_key != "" ? aws_key_pair.deployer[0].key_name : (var.key_name != "" ? var.key_name : null)
  iam_instance_profile        = var.ai_assist ? aws_iam_instance_profile.app[0].name : null

  # Compressed so the rendered script stays well under EC2's 16 KB user_data limit
  user_data_base64            = base64gzip(local.user_data)
  user_data_replace_on_change = true

  metadata_options {
    http_tokens   = "required"
    http_endpoint = "enabled"
  }

  root_block_device {
    volume_size           = var.root_volume_gb
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = true
    tags = {
      Name = "${var.project_name}-root-disk"
    }
  }

  tags = {
    Name = "${var.project_name}-server"
  }
}

# ------------------------------------------------------------------------------
# AI reader: S3 bucket for scans while Textract reads them
# ------------------------------------------------------------------------------
resource "aws_s3_bucket" "scans" {
  count         = var.ai_assist ? 1 : 0
  bucket        = "${var.project_name}-scans-${data.aws_caller_identity.current.account_id}-${var.aws_region}"
  force_destroy = true # holds nothing worth keeping: scans live here only while Textract reads them

  tags = {
    Name = "${var.project_name}-scans"
  }
}

resource "aws_s3_bucket_public_access_block" "scans" {
  count                   = var.ai_assist ? 1 : 0
  bucket                  = aws_s3_bucket.scans[0].id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "scans" {
  count  = var.ai_assist ? 1 : 0
  bucket = aws_s3_bucket.scans[0].id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# The gateway deletes each scan as soon as Textract is done; this rule is the backstop
resource "aws_s3_bucket_lifecycle_configuration" "scans" {
  count  = var.ai_assist ? 1 : 0
  bucket = aws_s3_bucket.scans[0].id

  rule {
    id     = "expire-scans"
    status = "Enabled"
    filter {}

    expiration {
      days = 1
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

resource "aws_s3_bucket_policy" "scans" {
  count      = var.ai_assist ? 1 : 0
  bucket     = aws_s3_bucket.scans[0].id
  depends_on = [aws_s3_bucket_public_access_block.scans]

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.scans[0].arn, "${aws_s3_bucket.scans[0].arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })
}

# ------------------------------------------------------------------------------
# AI reader: the instance's IAM role (Bedrock, Textract, the scans bucket)
# ------------------------------------------------------------------------------
resource "aws_iam_role" "app" {
  count = var.ai_assist ? 1 : 0
  name  = "${var.project_name}-instance"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "ai_reader" {
  count = var.ai_assist ? 1 : 0
  name  = "ai-reader"
  role  = aws_iam_role.app[0].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ClaudeOnBedrock"
        Effect   = "Allow"
        Action   = ["bedrock-mantle:CreateInference"]
        Resource = "*"
      },
      {
        Sid      = "TextractAnalysis" # Textract has no resource-level permissions for these actions
        Effect   = "Allow"
        Action   = ["textract:StartDocumentAnalysis", "textract:GetDocumentAnalysis"]
        Resource = "*"
      },
      {
        Sid      = "ScanStaging"
        Effect   = "Allow"
        Action   = ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"]
        Resource = "${aws_s3_bucket.scans[0].arn}/scans/*"
      },
    ]
  })
}

resource "aws_iam_instance_profile" "app" {
  count = var.ai_assist ? 1 : 0
  name  = "${var.project_name}-instance"
  role  = aws_iam_role.app[0].name
}
