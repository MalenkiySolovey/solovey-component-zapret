//go:build !linux

package strategy

func currentEngineArgBudget() (engineArgBudget, error) {
	// This compiler can preview a Linux definition off-host. No local
	// execution limit is evidence about the destination Linux Host.
	return engineArgBudget{total: linuxArgFloor - engineLauncherReserve, maxString: linuxArgFloor}, nil
}
