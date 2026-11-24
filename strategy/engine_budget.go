package strategy

import (
	"errors"
	"fmt"
)

// Linux execve guarantees a 128 KiB aggregate floor on the supported
// architectures. Leave 4 KiB for the broker-owned executable, flags and
// environment. Charge a NUL and a 64-bit argv pointer per compiled argument.
const engineArgumentBudget = (128 - 4) << 10
const engineArgumentOverhead = 1 + 8

// Every native profile requires at least its exact mark and one protocol port.
const minimumEngineProfileBytes = len("--filter-mark=0x1/0xffffffff") + len("--filter-tcp=1") + 2*engineArgumentOverhead

var ErrEngineArgumentBudget = errors.New("Zapret native engine argument budget exceeded")

func argumentBudgetError(bytes int) error {
	return fmt.Errorf("%w: requires at least %d bytes, limit %d; reduce the selected profiles, lists or assets", ErrEngineArgumentBudget, bytes, engineArgumentBudget)
}

func chargeEngineArgs(used *int, args []string) error {
	for _, arg := range args {
		*used += len(arg) + engineArgumentOverhead
		if *used > engineArgumentBudget {
			return argumentBudgetError(*used)
		}
	}
	return nil
}
