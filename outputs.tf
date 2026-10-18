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

output "app_url" {
  description = "The C23 Decision Desk in the browser (frontend; the API is served from the same origin)"
  value       = "http://${aws_instance.app.public_ip}/"
}

output "backend_api_url" {
  description = "Base URL of the Go Backend API Gateway, through nginx"
  value       = "http://${aws_instance.app.public_ip}/api/v1"
}

output "health_check_url" {
  description = "Liveness probe endpoint"
  value       = "http://${aws_instance.app.public_ip}/health"
}

output "ready_check_url" {
  description = "Deep readiness check endpoint (verifies PostgreSQL, FastAPI, and Redis)"
  value       = "http://${aws_instance.app.public_ip}/ready"
}

output "database_url_on_instance" {
  description = "Connection string the gateway uses; PostgreSQL is reachable only from the instance itself"
  value       = "postgres://${var.db_user}:${urlencode(var.db_password)}@127.0.0.1:5432/${var.db_name}?sslmode=disable"
  sensitive   = true
}

output "decision_engine_docs_tunnel" {
  description = "The engine listens on localhost only; open its Swagger docs through an SSH tunnel, then browse http://localhost:8000/docs"
  value       = "ssh -L 8000:127.0.0.1:${var.decision_engine_port} ubuntu@${aws_instance.app.public_ip}"
}

output "ssh_command" {
  description = "Command to SSH into the instance (works only from CIDRs in allowed_ssh_cidr)"
  value       = "ssh ubuntu@${aws_instance.app.public_ip}"
}

output "deployment_log" {
  description = "First boot takes several minutes (toolchains, builds). Follow it over SSH with this command"
  value       = "ssh ubuntu@${aws_instance.app.public_ip} sudo tail -f /var/log/c23-deploy.log"
}

output "first_sign_in" {
  description = "How to create the first admin and token when auth_mode is required"
  value       = var.auth_mode == "required" ? "ssh in, then: cd /opt/c23/backend && sudo -u c23 ./bin/api bootstrap -email you@example.com -workspace \"Finance\"" : "auth is off: no sign-in needed"
}
