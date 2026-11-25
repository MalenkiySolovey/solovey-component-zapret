package strategy

import (
	"slices"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

// Keep the old sealed representation for scopes that already fit it. Wide
// ranges and larger sparse sets never expand to a per-port representation.
const legacyExplicitPorts = 64

func sealPorts(ports []uint16, ranges []intent.PortInterval) ([]uint16, []intent.PortInterval, error) {
	if len(ports) != 0 && len(ranges) != 0 {
		return nil, nil, ErrInvalidStrategy
	}
	intervals, err := intent.CanonicalPortIntervals(ports, ranges...)
	if err != nil {
		return nil, nil, err
	}
	count := 0
	for _, interval := range intervals {
		count += int(interval.Last) - int(interval.First) + 1
	}
	if len(ports) != 0 && len(ports) != count {
		return nil, nil, ErrInvalidStrategy
	}
	if count > legacyExplicitPorts {
		return nil, intervals, nil
	}
	if count == 0 {
		return nil, nil, nil
	}
	ports = make([]uint16, 0, count)
	for _, interval := range intervals {
		for port := uint32(interval.First); port <= uint32(interval.Last); port++ {
			ports = append(ports, uint16(port))
		}
	}
	return ports, nil, nil
}

func hasPorts(ports []uint16, ranges []intent.PortInterval) bool {
	return len(ports) != 0 || len(ranges) != 0
}

func containsPort(port uint16, ports []uint16, ranges []intent.PortInterval) bool {
	return slices.Contains(ports, port) || slices.ContainsFunc(ranges, func(r intent.PortInterval) bool {
		return r.First <= port && port <= r.Last
	})
}

func unionPorts(ports []uint16, ranges []intent.PortInterval) ([]uint16, []intent.PortInterval) {
	if len(ranges) == 0 {
		slices.Sort(ports)
		ports = slices.Compact(ports)
		if len(ports) <= legacyExplicitPorts {
			return ports, nil
		}
	}
	// Only sealed, positive intervals reach a packet scope.
	intervals, _ := intent.CanonicalPortIntervals(ports, ranges...)
	return nil, intervals
}
