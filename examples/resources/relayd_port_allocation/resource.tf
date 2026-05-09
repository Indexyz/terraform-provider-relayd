variable "relay_host" {
  description = "Public host or IP address where relayd accepts client connections."
  type        = string
  default     = "relay.example.com"
}

resource "relayd_port_allocation" "example" {
  protocol = "both"
}

output "allocated_port" {
  description = "Port reserved by relayd for this allocation. With protocol both, TCP and UDP share this port."
  value       = relayd_port_allocation.example.port
}

output "relay_endpoint" {
  description = "Host:port clients can use after relayd allocates the listen port."
  value       = "${var.relay_host}:${relayd_port_allocation.example.port}"
}
