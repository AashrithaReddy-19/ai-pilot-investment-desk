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
  description = "EC2 instance type. One host runs nginx, the Go gateway, the Python engine, PostgreSQL and Redis; t3.small (2 vCPUs, 2 GB RAM) plus swap is enough"
  type        = string
  default     = "t3.small"
}

variable "root_volume_gb" {
  description = "Root disk size in GB (holds the OS, toolchains, build caches and the PostgreSQL data)"
  type        = number
  default     = 20
}

variable "swap_gb" {
  description = "Swap file size in GB (0 = none). Keeps the Go and Vite builds from running out of memory on small instances"
  type        = number
  default     = 2
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

variable "allowed_http_cidr" {
  description = "CIDR blocks allowed to reach the app on port 80 (frontend and API through nginx)"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "allowed_origins" {
  description = "Browser origins allowed to call the API cross-origin: \"*\" for any origin (handled by nginx), or comma-separated exact origins (handled by the gateway). The bundled frontend is served from the same origin and needs none."
  type        = string
  default     = ""

  validation {
    condition     = var.allowed_origins == "*" || !strcontains(var.allowed_origins, "*")
    error_message = "allowed_origins is either \"*\" on its own or a list of exact origins without wildcards."
  }
}

variable "app_port" {
  description = "Local port of the Go Backend API Gateway (behind nginx, not opened publicly)"
  type        = number
  default     = 8080
}

variable "decision_engine_port" {
  description = "Local port of the Python FastAPI Decision Engine (reached by the gateway only)"
  type        = number
  default     = 8000
}

# Database defaults match backend/.env.example: postgres://c23:CHANGE_ME@localhost:5432/c23
variable "db_name" {
  description = "PostgreSQL database created on the instance"
  type        = string
  default     = "c23"

  validation {
    condition     = can(regex("^[a-z_][a-z0-9_]*$", var.db_name))
    error_message = "db_name must be lowercase letters, digits and underscores."
  }
}

variable "db_user" {
  description = "PostgreSQL role the gateway connects as"
  type        = string
  default     = "c23"

  validation {
    condition     = can(regex("^[a-z_][a-z0-9_]*$", var.db_user))
    error_message = "db_user must be lowercase letters, digits and underscores."
  }
}

variable "db_password" {
  description = "Password for the PostgreSQL role. PostgreSQL listens on localhost only, but change it for anything beyond a demo."
  type        = string
  default     = "CHANGE_ME"
  sensitive   = true
}

variable "repo_url" {
  description = "Git repository URL to clone onto the instance"
  type        = string
  default     = "https://github.com/AashrithaReddy-19/ai-pilot-investment-desk.git"
}

variable "git_token" {
  description = "GitHub token with read access to repo_url, needed only when the repository is private (a fine-grained token limited to that repo, Contents: Read-only). Empty = public repo."
  type        = string
  default     = ""
  sensitive   = true
}

variable "git_ref" {
  description = "Branch or tag to deploy"
  type        = string
  default     = "main"
}

variable "go_version" {
  description = "Go toolchain installed from go.dev to build the gateway (backend/go.mod needs 1.25 or newer; Ubuntu's own package is too old)"
  type        = string
  default     = "1.26.4"
}

variable "node_major" {
  description = "Node.js major version (latest release of it from nodejs.org) used to build the frontend"
  type        = number
  default     = 24
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
  description = "Gateway sign-in: off (single default workspace, no sign-in; the bundled frontend has no sign-in screen, so it needs this) or required (bearer tokens and workspaces, API clients only)"
  type        = string
  default     = "off"

  validation {
    condition     = contains(["required", "off"], var.auth_mode)
    error_message = "auth_mode must be required or off."
  }
}

variable "rate_limit_per_minute" {
  description = "Requests per minute per client IP on /api, /health and /ready, enforced by nginx (0 disables)"
  type        = number
  default     = 600
}

variable "ai_assist" {
  description = "Balance-sheet AI reader: Claude on Amazon Bedrock proposes AI-spend rows when the ordinary reader finds none (or the reviewer asks), and Amazon Textract reads scanned PDFs. Creates an S3 bucket for scans and an IAM role for the instance. Enable access to the Bedrock model in the AWS console first."
  type        = bool
  default     = true
}

variable "bedrock_model" {
  description = "Bedrock model id for the AI reader"
  type        = string
  default     = "anthropic.claude-sonnet-5-5"
}

variable "bedrock_region" {
  description = "Region of the Bedrock endpoint (empty = aws_region). In ap-south-1 Claude is served through Bedrock's global endpoint, so a request may be processed in another region."
  type        = string
  default     = ""
}

variable "ai_timeout_seconds" {
  description = "Most time one AI reading may take (Textract plus the model), on top of the ordinary reader's 60 seconds"
  type        = number
  default     = 240

  validation {
    condition     = var.ai_timeout_seconds >= 10 && var.ai_timeout_seconds <= 900
    error_message = "ai_timeout_seconds must be between 10 and 900."
  }
}

variable "textract_max_pages" {
  description = "Largest scanned PDF sent to Textract, in pages (table analysis is billed per page, about $0.015)"
  type        = number
  default     = 100
}
