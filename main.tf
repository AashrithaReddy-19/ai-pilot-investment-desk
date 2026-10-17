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
# Security Group
# ------------------------------------------------------------------------------
resource "aws_security_group" "app" {
  name        = "${var.project_name}-sg"
  description = "Security group for C23 Decision Desk EC2 instance"
  vpc_id      = data.aws_vpc.default.id

  # Inbound SSH
  ingress {
    description = "SSH Access"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.allowed_ssh_cidr
  }

  # Inbound Backend API Gateway
  ingress {
    description = "Go Backend API Gateway"
    from_port   = var.app_port
    to_port     = var.app_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Inbound Decision Engine
  ingress {
    description = "Python FastAPI Decision Engine"
    from_port   = var.decision_engine_port
    to_port     = var.decision_engine_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Outbound All Traffic
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
# EC2 Instance & Automated Deployment
# ------------------------------------------------------------------------------
resource "aws_instance" "app" {
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = var.instance_type
  subnet_id                   = data.aws_subnets.default.ids[0]
  vpc_security_group_ids      = [aws_security_group.app.id]
  associate_public_ip_address = true
  key_name                    = var.public_key != "" ? aws_key_pair.deployer[0].key_name : (var.key_name != "" ? var.key_name : null)

  root_block_device {
    volume_size           = 20
    volume_type           = "gp3"
    delete_on_termination = true
    tags = {
      Name = "${var.project_name}-root-disk"
    }
  }

  user_data = <<-EOF
    #!/usr/bin/env bash
    set -euxo pipefail

    exec > >(tee -a /var/log/user-data.log) 2>&1
    echo "=== Starting C23 Deployment on AWS EC2 ==="

    # 1. System Updates & Core Packages
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y --no-install-recommends \
      golang-go python3 python3-venv python3-pip \
      postgresql postgresql-contrib redis-server \
      git curl jq ca-certificates

    # 2. Configure & Start Redis
    systemctl enable redis-server
    systemctl restart redis-server

    # 3. Configure & Start PostgreSQL
    systemctl enable postgresql
    systemctl restart postgresql

    sudo -u postgres psql -c "DO \$\$
    BEGIN
      IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'c23') THEN
        CREATE ROLE c23 LOGIN PASSWORD '${var.db_password}';
      ELSE
        ALTER ROLE c23 WITH PASSWORD '${var.db_password}';
      END IF;
    END
    \$\$;"

    sudo -u postgres psql -c "SELECT 'CREATE DATABASE c23 OWNER c23' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'c23')\gexec"
    sudo -u postgres psql -c "SELECT 'CREATE DATABASE c23_test OWNER c23' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'c23_test')\gexec"
    sudo -u postgres psql -d c23 -c "GRANT ALL ON SCHEMA public TO c23;"
    sudo -u postgres psql -d c23_test -c "GRANT ALL ON SCHEMA public TO c23;"

    # 4. Clone Repository
    mkdir -p /opt/c23
    if [ ! -d "/opt/c23/.git" ]; then
      git clone "${var.repo_url}" /opt/c23
    else
      cd /opt/c23 && git pull origin main
    fi
    cd /opt/c23

    # 5. Set up Python Decision Engine
    cd /opt/c23/decision-engine
    python3 -m venv venv
    ./venv/bin/pip install --upgrade pip
    ./venv/bin/pip install -r requirements.txt

    cat << 'SERVICE_DE' > /etc/systemd/system/c23-decision-engine.service
    [Unit]
    Description=C23 Decision Engine (FastAPI)
    After=network.target

    [Service]
    Type=simple
    User=root
    WorkingDirectory=/opt/c23/decision-engine
    ExecStart=/opt/c23/decision-engine/venv/bin/uvicorn app.main:app --host 0.0.0.0 --port ${var.decision_engine_port}
    Restart=always
    RestartSec=5

    [Install]
    WantedBy=multi-user.target
    SERVICE_DE

    systemctl daemon-reload
    systemctl enable c23-decision-engine
    systemctl restart c23-decision-engine

    # 6. Set up Go Backend API Gateway
    cat << ENV_BACKEND > /opt/c23/backend/.env
    PORT=${var.app_port}
    DATABASE_URL=postgres://c23:${var.db_password}@localhost:5432/c23?sslmode=disable
    REDIS_URL=redis://localhost:6379/0
    RULES_ENGINE_URL=http://localhost:${var.decision_engine_port}
    RULES_VERSION=1
    ALLOWED_ORIGINS=*
    REQUEST_TIMEOUT_SECONDS=5
    CACHE_TTL_SECONDS=1800
    MAX_BODY_BYTES=1048576
    AUTO_MIGRATE=true
    GIN_MODE=release
    ENV_BACKEND

    cd /opt/c23/backend
    mkdir -p bin
    go build -o bin/api ./cmd/api

    # Run initial migrations
    ./bin/api migrate up

    cat << 'SERVICE_BE' > /etc/systemd/system/c23-backend.service
    [Unit]
    Description=C23 Backend API Gateway (Go Gin)
    After=network.target postgresql.service redis-server.service c23-decision-engine.service

    [Service]
    Type=simple
    User=root
    WorkingDirectory=/opt/c23/backend
    ExecStart=/opt/c23/backend/bin/api
    Restart=always
    RestartSec=5
    EnvironmentFile=/opt/c23/backend/.env

    [Install]
    WantedBy=multi-user.target
    SERVICE_BE

    systemctl daemon-reload
    systemctl enable c23-backend
    systemctl restart c23-backend

    echo "=== C23 Deployment Completed Successfully ==="
  EOF

  tags = {
    Name = "${var.project_name}-server"
  }
}
