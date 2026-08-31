package datasource

type Source interface {
	Read() error
}
