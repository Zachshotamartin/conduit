package executor

import (
	_ "example.com/conduitfixture/internal/datasource"
	_ "example.com/conduitfixture/internal/datasource/postgres"
	_ "example.com/conduitfixture/internal/protocol"
	_ "example.com/conduitfixture/internal/queue"
	_ "example.com/conduitfixture/internal/transport"
)
