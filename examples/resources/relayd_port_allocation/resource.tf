resource "relayd_port_allocation" "example" {
  protocol    = "tcp"
  target_port = 8080
  host        = "127.0.0.1"
}
