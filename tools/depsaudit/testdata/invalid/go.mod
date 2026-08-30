module example.com/depsinvalid

go 1.23.0

require (
	example.com/rogue v0.0.0
	github.com/coder/websocket v0.0.0
	github.com/nats-io/nats.go v0.0.0
)

replace example.com/rogue => ./stubs/example.com/rogue

replace github.com/coder/websocket => ./stubs/github.com/coder/websocket

replace github.com/nats-io/nats.go => ./stubs/github.com/nats-io/nats.go
