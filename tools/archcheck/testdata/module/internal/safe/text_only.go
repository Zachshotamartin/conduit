package safe

// These are deliberately not imports or calls. A forbidden-text scanner will
// report them even though the Go package graph and syntax tree do not.
const decoys = `
github.com/coder/websocket
github.com/vektah/gqlparser/v2
github.com/nats-io/nats.go
github.com/jackc/pgx/v5
go.opentelemetry.io/otel
time.Now()
runtime.GOOS
//go:build linux
`
