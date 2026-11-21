// Package binding resolves the Outbounds owner's single desired attachment
// into a Strategy snapshot and a resource-bounded Network Planner intent.
package binding

import (
	"errors"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

var (
	ErrStrategyMissing = errors.New("Zapret strategy unavailable")
	ErrComponentAbsent = errors.New("Zapret component absent or disabled")
)

type DirectOutbound struct {
	Host              hostref.Ref
	Ref, Type, Detour string
	Revision          string
}

type StrategySource interface {
	LookupStrategy(ref string) (strategy.Strategy, bool)
}

type Resolution struct {
	Strategy strategy.Strategy
	Intent   intent.ZapretPacketPathIntent
}

// Resolve returns nil, nil only when the valid binding is explicitly OFF.
func Resolve(binding outbounds.ZapretBinding, outbound DirectOutbound, source StrategySource, enabled bool) (*Resolution, error) {
	if err := binding.Validate(outbound.Ref, outbound.Type, outbound.Detour); err != nil {
		return nil, err
	}
	if !binding.Enabled {
		return nil, nil
	}
	if !enabled {
		return nil, ErrComponentAbsent
	}
	if source == nil {
		return nil, ErrStrategyMissing
	}
	selected, ok := source.LookupStrategy(binding.StrategyRef)
	if !ok || selected.ID != binding.StrategyRef {
		return nil, ErrStrategyMissing
	}
	selected, err := selected.Verified()
	if err != nil {
		return nil, err
	}
	scope, err := selected.PacketScope()
	if err != nil {
		return nil, err
	}
	value := intent.ZapretPacketPathIntent{
		Schema: intent.ZapretPacketPathSchema, HostRef: outbound.Host, Owner: intent.ZapretOwner,
		ResourceRef: intent.ZapretResourcePrefix + outbound.Ref, OutboundRef: outbound.Ref, StrategyRef: selected.ID,
		StrategyRevision: selected.Revision, DesiredRevision: selected.Revision, InputRevision: outbound.Revision,
		Purpose: intent.ZapretPurpose, TCPPorts: scope.TCP, UDPPorts: scope.UDP, NeedsReturn: scope.NeedsReturn,
		Directions: []intent.PacketDirection{intent.PacketDirectionOutbound},
	}
	if len(scope.TCP) != 0 {
		value.Protocols = append(value.Protocols, intent.PacketProtocolTCP)
	}
	if len(scope.UDP) != 0 {
		value.Protocols = append(value.Protocols, intent.PacketProtocolUDP)
	}
	if value.NeedsReturn {
		value.Directions = append(value.Directions, intent.PacketDirectionInbound)
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &Resolution{Strategy: selected, Intent: intent.CanonicalZapretPacketPathIntent(value)}, nil
}
