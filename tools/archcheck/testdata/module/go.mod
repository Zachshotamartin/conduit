module example.com/conduitfixture

go 1.23.0

require (
	github.com/coder/websocket v0.0.0
	github.com/jackc/pgx/v5 v5.0.0
	github.com/nats-io/nats.go v0.0.0
	github.com/prometheus/client_golang v0.0.0
	github.com/vektah/gqlparser/v2 v2.0.0
	go.opentelemetry.io/otel v0.0.0
)

replace github.com/coder/websocket => ./stubs/github.com/coder/websocket

replace github.com/jackc/pgx/v5 => ./stubs/github.com/jackc/pgx/v5

replace github.com/nats-io/nats.go => ./stubs/github.com/nats-io/nats.go

replace github.com/prometheus/client_golang => ./stubs/github.com/prometheus/client_golang

replace github.com/vektah/gqlparser/v2 => ./stubs/github.com/vektah/gqlparser/v2

replace go.opentelemetry.io/otel => ./stubs/go.opentelemetry.io/otel
