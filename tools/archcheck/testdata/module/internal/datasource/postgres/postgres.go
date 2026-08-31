package postgres

import (
	"example.com/conduitfixture/internal/datasource"
	_ "github.com/jackc/pgx/v5"
)

var _ datasource.Source = Adapter{}

type Adapter struct{}

func (Adapter) Read() error { return nil }
