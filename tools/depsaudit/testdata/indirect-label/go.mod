module example.com/depsindirect

go 1.23.0

require example.com/rogue v0.0.0 // indirect

replace example.com/rogue => ./stubs/example.com/rogue
