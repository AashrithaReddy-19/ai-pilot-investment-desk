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
  description = "CIDR blocks allowed to SSH into the instance, e.g. [\"203.0.113.4/32\"]. Empty (default) = no SSH ingress."
  type        = list(string)
  default     = []
}

variable "decision_engine_public_cidr" {
  description = "CIDR blocks allowed to call the decision engine directly. Empty (default) = engine reachable only from the instance itself."
  type        = list(string)
  default     = []
}

variable "allowed_origins" {
  description = "Comma-separated exact browser origins allowed by the gateway's CORS (e.g. \"https://desk.example.com\"). Wildcards are not supported. Empty = same-origin only."
  type        = string
  default     = ""
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

variable "auth_mode" {
  description = "Gateway sign-in: required (bearer tokens and workspaces, the default for a public server) or off (single default workspace, no sign-in)"
  type        = string
  default     = "required"

  validation {
    condition     = contains(["required", "off"], var.auth_mode)
    error_message = "auth_mode must be required or off."
  }
}

variable "rate_limit_per_minute" {
  description = "Gateway requests per minute per client IP (0 disables rate limiting)"
  type        = number
  default     = 600
}
