resource "relayd_port_allocation" "example" {
  protocol = "tcp"
}

resource "relayd_port_binding" "example" {
  allocation_id = relayd_port_allocation.example.id
  host          = "127.0.0.1"
  target_port   = 8080
}
