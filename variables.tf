variable "aws_region" {
  description = "The AWS region to deploy the instance in"
  type        = string
  default     = "us-east-1"
}

variable "aws_access_key" {
  description = "AWS Access Key ID (leave blank if configuring via AWS CLI or environment variables)"
  type        = string
  default     = ""
  sensitive   = true
}

variable "aws_secret_key" {
  description = "AWS Secret Access Key (leave blank if configuring via AWS CLI or environment variables)"
  type        = string
  default     = ""
  sensitive   = true
}

variable "instance_type" {
  description = "EC2 instance type (t3.small provides 2 vCPUs and 2 GB RAM, ideal for dual Go+Python services)"
  type        = string
  default     = "t3.small"
}

variable "key_name" {
  description = "Name of an existing AWS Key Pair (optional; leave blank if using public_key or no SSH key)"
  type        = string
  default     = ""
}

variable "public_key" {
  description = "SSH public key string (e.g. 'ssh-ed25519 AAAAC3...'). If provided, creates a new key pair."
  type        = string
  default     = ""
}

variable "allowed_ssh_cidr" {
  description = "CIDR block allowed to SSH into the instance"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "app_port" {
  description = "Port for the Go Backend API Gateway"
  type        = number
  default     = 8080
}

variable "decision_engine_port" {
  description = "Port for the Python FastAPI Decision Engine"
  type        = number
  default     = 8000
}

variable "db_password" {
  description = "Password for PostgreSQL role c23"
  type        = string
  default     = "c23SecurePassword123"
  sensitive   = true
}

variable "repo_url" {
  description = "Git repository URL to clone onto the instance"
  type        = string
  default     = "https://github.com/AashrithaReddy-19/ai-pilot-investment-desk.git"
}

variable "project_name" {
  description = "Project name tag and resource prefix"
  type        = string
  default     = "c23-pilot-desk"
}

variable "environment" {
  description = "Deployment environment (production, staging, dev)"
  type        = string
  default     = "production"
}
