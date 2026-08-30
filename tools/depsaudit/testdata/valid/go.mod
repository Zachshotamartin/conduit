module example.com/depsvalid

go 1.23.0

require (
	example.com/testhelper v0.0.0
	github.com/coder/websocket v0.0.0
)

replace example.com/testhelper => ./stubs/example.com/testhelper

replace github.com/coder/websocket => ./stubs/github.com/coder/websocket
