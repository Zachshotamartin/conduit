module example.com/mismatchedversion

go 1.23.0

require github.com/coder/websocket v1.0.0

replace github.com/coder/websocket => ./stubs/github.com/coder/websocket
