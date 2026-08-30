package valid

import (
	"math/rand"
	"time"
)

func exerciseDeterministicInputs() {
	local := rand.New(rand.NewSource(42))
	_ = local.Int()
	_ = time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC)
}
