output "instance_id" {
  description = "ID of the provisioned EC2 instance"
  value       = aws_instance.app.id
}

output "instance_public_ip" {
  description = "Public IPv4 address of the EC2 instance"
  value       = aws_instance.app.public_ip
}

output "instance_public_dns" {
  description = "Public DNS name of the EC2 instance"
  value       = aws_instance.app.public_dns
}

output "backend_api_url" {
  description = "Base URL of the Go Backend API Gateway"
  value       = "http://${aws_instance.app.public_ip}:${var.app_port}"
}

output "health_check_url" {
  description = "Liveness probe endpoint"
  value       = "http://${aws_instance.app.public_ip}:${var.app_port}/health"
}

output "ready_check_url" {
  description = "Deep readiness check endpoint (verifies PostgreSQL, FastAPI, and Redis)"
  value       = "http://${aws_instance.app.public_ip}:${var.app_port}/ready"
}

output "decision_engine_url" {
  description = "Base URL of the Python FastAPI Decision Engine (reachable only from CIDRs in decision_engine_public_cidr)"
  value       = "http://${aws_instance.app.public_ip}:${var.decision_engine_port}"
}

output "decision_engine_docs" {
  description = "Swagger OpenAPI Documentation URL for the Decision Engine"
  value       = "http://${aws_instance.app.public_ip}:${var.decision_engine_port}/docs"
}

output "ssh_command" {
  description = "Command to SSH into the instance (works only from CIDRs in allowed_ssh_cidr)"
  value       = "ssh ubuntu@${aws_instance.app.public_ip}"
}

output "first_sign_in" {
  description = "How to create the first admin and token when auth_mode is required"
  value       = var.auth_mode == "required" ? "ssh in, then: cd /opt/c23/backend && ./bin/api bootstrap -email you@example.com -workspace \"Finance\"" : "auth is off: no sign-in needed"
}
