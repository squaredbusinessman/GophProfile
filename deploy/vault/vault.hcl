ui = true
disable_mlock = true

listener "tcp" {
  address = "0.0.0.0:8200"
  tls_disable = true
}

storage "postgresql" {
  connection_url = "postgres://vault:vault@postgres:5432/vault?sslmode=disable"
}

api_addr = "http://127.0.0.1:8200"
