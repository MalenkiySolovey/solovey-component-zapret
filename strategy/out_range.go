package strategy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

var errOutRangeSyntax = errors.New("outgoing range requires a, x, or a counter range such as -d3 or n2<n8")
var errOutRangeProtocol = errors.New("outgoing relative sequence counters s and p require TCP")

// Only documented packet positions are accepted, not the trailing junk
// tolerated by the engine's scanf-based parser.
func sealOutRange(value string, protocol intent.PacketProtocol) (string, error) {
	switch value {
	case "", "a":
		return "", nil // The pinned outgoing default is always.
	case "x":
		return value, nil
	}
	i := strings.IndexAny(value, "-<")
	if i < 0 || i == len(value)-1 && (i == 0 || value[i] != '-') {
		return "", errOutRangeSyntax
	}
	for _, endpoint := range [2]string{value[:i], value[i+1:]} {
		if endpoint == "" {
			continue
		}
		if len(endpoint) < 2 || len(endpoint) > len("n4294967295") {
			return "", errOutRangeSyntax
		}
		bits := 32
		switch endpoint[0] {
		case 'n', 'd', 'b':
		case 's', 'p':
			if protocol != intent.PacketProtocolTCP {
				return "", errOutRangeProtocol
			}
			bits = 31 // Native relative sequence positions are limited to 2 GiB.
		default:
			return "", errOutRangeSyntax
		}
		if _, err := strconv.ParseUint(endpoint[1:], 10, bits); err != nil {
			return "", errOutRangeSyntax
		}
	}
	return value, nil
}

func (p *Profile) sealOutRanges() error {
	if p.OutRange != "" && p.SplitAt == 0 && p.QUICRepeats == 0 && !p.Pass {
		return errors.New("profile outgoing range requires a split, QUIC or pass action; set HTTP ranges on each action")
	}
	var err error
	protocol := p.Protocol
	if p.Pass && len(p.UDPPorts) != 0 {
		protocol = intent.PacketProtocolUDP
	}
	if p.OutRange, err = sealOutRange(p.OutRange, protocol); err != nil {
		return err
	}
	for i := range p.HTTPActions {
		action := &p.HTTPActions[i]
		if action.OutRange, err = sealOutRange(action.OutRange, p.Protocol); err != nil {
			return fmt.Errorf("HTTP action %d: %w", i+1, err)
		}
	}
	return nil
}

// nfqws2 snapshots the filter at each action. Reset it explicitly when
// a following action in the same profile needs the outgoing default.
func appendOutRange(args []string, current, next string) []string {
	if current == next {
		return args
	}
	if next == "" {
		next = "a"
	}
	return append(args, "--out-range="+next)
}
