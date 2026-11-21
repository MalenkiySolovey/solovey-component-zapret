package binding

import (
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

type source map[string]strategy.Strategy

func (s source) LookupStrategy(ref string) (strategy.Strategy, bool) {
	value, ok := s[ref]
	return value, ok
}

func testStrategy(t *testing.T) strategy.Strategy {
	t.Helper()
	value, err := strategy.Seal(strategy.Strategy{
		Schema: strategy.Schema, ID: "strategy-a",
		Profiles: []strategy.Profile{{ID: "passive-tcp", Protocol: intent.PacketProtocolTCP, Ports: []uint16{443}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestResolveOffDoesNotNeedComponentOrStrategy(t *testing.T) {
	result, err := Resolve(outbounds.ZapretBinding{OutboundRef: "direct-a"}, DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct"}, nil, false)
	if err != nil || result != nil {
		t.Fatalf("OFF binding did not remain ordinary Direct: %#v %v", result, err)
	}
}

func TestResolveFailsClosedForMissingComponentStrategyAndDetour(t *testing.T) {
	value := testStrategy(t)
	base := outbounds.ZapretBinding{OutboundRef: "direct-a", Enabled: true, StrategyRef: value.ID}
	if _, err := Resolve(base, DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct", Detour: "proxy"}, source{"strategy-a": value}, true); !errors.Is(err, outbounds.ErrZapretDirectRequired) {
		t.Fatalf("detoured Direct was admitted: %v", err)
	}
	if _, err := Resolve(base, DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct"}, nil, true); !errors.Is(err, ErrStrategyMissing) {
		t.Fatalf("missing Strategy was not rejected: %v", err)
	}
	if _, err := Resolve(base, DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct"}, source{"strategy-a": value}, false); !errors.Is(err, ErrComponentAbsent) {
		t.Fatalf("absent component was not rejected: %v", err)
	}
}

func TestResolveProjectsOneBoundedIntent(t *testing.T) {
	value := testStrategy(t)
	result, err := Resolve(outbounds.ZapretBinding{OutboundRef: "direct-a", Enabled: true, StrategyRef: value.ID}, DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct", Revision: strings.Repeat("b", 64)}, source{"strategy-a": value}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent.ResourceRef != "outbound:direct-a" || result.Intent.StrategyRef != "strategy-a" || result.Intent.TCPPorts[0] != 443 {
		t.Fatalf("unexpected packet-path intent: %#v", result.Intent)
	}
	if err := result.Intent.Validate(); err != nil {
		t.Fatal(err)
	}
}
