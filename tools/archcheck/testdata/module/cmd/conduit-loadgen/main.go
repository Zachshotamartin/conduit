package main

import (
	_ "example.com/conduitfixture/internal/bus/memory"
	_ "example.com/conduitfixture/internal/fanout"
	_ "example.com/conduitfixture/internal/queue"
	_ "example.com/conduitfixture/internal/registry"
)

func main() {}
