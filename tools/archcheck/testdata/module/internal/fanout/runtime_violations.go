package fanout

import (
	"math/rand"
	"runtime"
	"time"
)

var (
	illegalGOOS   = runtime.GOOS
	illegalNow    = time.Now()
	illegalAfter  = time.After(time.Second)
	illegalRandom = rand.Int()
)
