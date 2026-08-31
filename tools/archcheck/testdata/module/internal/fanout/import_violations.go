package fanout

import (
	_ "example.com/conduitfixture/test/fixtures"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/nats-io/nats.go"
	_ "github.com/prometheus/client_golang/prometheus"
	_ "github.com/vektah/gqlparser/v2"
	_ "go.opentelemetry.io/otel"
)
