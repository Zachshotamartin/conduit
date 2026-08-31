package invalid

import (
	. "crypto/rand"
	random "math/rand"
	clk "time"
)

func exerciseNondeterminism() {
	clk.Sleep(1)
	_ = clk.Now()
	_ = clk.AfterFunc(1, func() {})
	_ = clk.NewTicker(1)
	_ = random.Int()
	_, _ = Read(nil)
}
