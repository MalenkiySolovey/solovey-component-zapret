package strategy

import (
	"errors"
	"fmt"
)

const (
	linuxArgFloor = 128 << 10
	// Space for the executable path, broker-owned flags and environment.
	engineLauncherReserve = 4 << 10
	// Count the trailing NUL and allow for a 64-bit kernel's argv pointer.
	engineArgumentOverhead = 1 + 8
)

// Every native profile requires at least its exact mark and one protocol port.
const minimumEngineProfileBytes = len("--filter-mark=0x1/0xffffffff") + len("--filter-tcp=1") + 2*engineArgumentOverhead

var ErrEngineArgumentBudget = errors.New("Zapret native engine argument budget exceeded")

type engineArgBudget struct {
	total, maxString int
}

func (b engineArgBudget) exceedsTotal(bytes int) error {
	return fmt.Errorf("%w: requires at least %d bytes, available %d after launcher overhead; reduce the selected profiles, lists or assets", ErrEngineArgumentBudget, bytes, b.total)
}

func (b engineArgBudget) charge(used *int, args []string) error {
	for _, arg := range args {
		if size := len(arg) + 1; size > b.maxString {
			return fmt.Errorf("%w: one argument needs %d bytes, Linux per-argument limit is %d", ErrEngineArgumentBudget, size, b.maxString)
		}
		*used += len(arg) + engineArgumentOverhead
		if *used > b.total {
			return b.exceedsTotal(*used)
		}
	}
	return nil
}
