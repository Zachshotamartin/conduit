module example.com/brokenvendor

go 1.23.0

require github.com/coder/websocket v0.0.0

replace github.com/coder/websocket => ./stubs/github.com/coder/websocket
