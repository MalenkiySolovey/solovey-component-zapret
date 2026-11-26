package strategy

import (
	"fmt"
	"os"
	"syscall"
)

func currentEngineArgBudget() (engineArgBudget, error) {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_STACK, &limit); err != nil {
		return engineArgBudget{}, fmt.Errorf("%w: cannot read launcher RLIMIT_STACK: %w", ErrEngineArgumentBudget, err)
	}
	return linuxEngineArgBudget(limit.Cur, os.Getpagesize()), nil
}

func linuxEngineArgBudget(softStack uint64, pageSize int) engineArgBudget {
	// Mirror bprm_stack_limits for Linux MMU builds: one quarter of the
	// soft stack limit, capped at three quarters of the 8 MiB kernel limit.
	total := max(linuxArgFloor, min(softStack/4, 6<<20))
	return engineArgBudget{total: int(total) - engineLauncherReserve, maxString: 32 * pageSize}
}
