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
locals {
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
  })
}

resource "aws_instance" "app" {
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = var.instance_type
  subnet_id                   = data.aws_subnets.default.ids[0]
  vpc_security_group_ids      = [aws_security_group.app.id]
  associate_public_ip_address = true
  key_name                    = var.public_key != "" ? aws_key_pair.deployer[0].key_name : (var.key_name != "" ? var.key_name : null)

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
