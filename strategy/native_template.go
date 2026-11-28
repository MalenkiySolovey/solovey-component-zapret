package strategy

import (
	"fmt"
	"slices"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

type nativeDraft struct {
	Profile
	l7, inRange, outRange string
	outRangePending       bool
	seen                  map[string]bool
	actionOrder           []NativeActionRef
	template              bool
	named                 bool
	familySet             bool
	inheritedActions      bool
	listRefs              NativeListRefs
	declaredListRefs      NativeListRefs
}

var errTemplateActionOrder = fmt.Errorf("%w: the typed runtime can import one complete action sequence per profile; move the combined actions into one profile or template", ErrInvalidStrategy)

func (d *nativeDraft) addFamily(family AddressFamily) {
	if !d.familySet {
		d.Family = family
	} else if d.Family != family {
		d.Family = ""
	}
	d.familySet = true
}

// The engine appends imported port terms, retaining the union of their nonzero intervals.
func (d *nativeDraft) addPortFilters(protocol intent.PacketProtocol, ports []intent.PortInterval) {
	if len(ports) == 0 {
		return
	}
	switch {
	case d.Protocol == "":
		d.Protocol, d.PortRanges = protocol, ports
	case d.Protocol == protocol:
		d.PortRanges, _ = intent.CanonicalPortIntervals(nil, slices.Concat(d.PortRanges, ports)...)
	case protocol == intent.PacketProtocolTCP:
		d.Protocol, d.PortRanges, d.UDPRanges = protocol, ports, d.PortRanges
	default:
		d.UDPRanges, _ = intent.CanonicalPortIntervals(nil, slices.Concat(d.UDPRanges, ports)...)
	}
}

// Pinned nfqws2 dp_copy copies set scalar filters and appends collections.
// Its action list contains snapshots, not the destination's parser state.
func (d *nativeDraft) importTemplate(from nativeDraft) error {
	if from.hasAction() {
		if d.hasAction() || d.seen["--payload"] || d.inRange != "" || d.outRangePending {
			return errTemplateActionOrder
		}
		d.inheritedActions = true
		d.SplitAt, d.QUICRepeats, d.QUICBlobRef, d.Pass = from.SplitAt, from.QUICRepeats, from.QUICBlobRef, from.Pass
		d.Payload, d.OutRange, d.inRange = from.Payload, from.OutRange, from.inRange
		d.Circular, d.HTTPActions, d.LuaActions = from.Circular, from.HTTPActions, from.LuaActions
		d.actionOrder = from.actionOrder
		d.seen["--payload"], d.seen["--lua-desync"] = from.seen["--payload"], from.seen["--lua-desync"]
	}
	d.addPortFilters(from.Protocol, from.PortRanges)
	d.addPortFilters(intent.PacketProtocolUDP, from.UDPRanges)
	if from.familySet {
		d.addFamily(from.Family)
	}
	if from.l7 != "" {
		d.l7 = from.l7
	}
	var err error
	d.Lists, err = mergeTemplateLists(d.Lists, from.Lists)
	if err != nil {
		return err
	}
	return d.listRefs.merge(from.listRefs)
}

func mergeTemplateLists(current, inherited *NativeLists) (*NativeLists, error) {
	if inherited == nil {
		return current, nil
	}
	if current == nil || current == inherited {
		return inherited, nil
	}
	return (NativeLists{
		HostList:        slices.Concat(current.HostList, inherited.HostList),
		HostListExclude: slices.Concat(current.HostListExclude, inherited.HostListExclude),
		IPSet:           slices.Concat(current.IPSet, inherited.IPSet),
		IPSetExclude:    slices.Concat(current.IPSetExclude, inherited.IPSetExclude),
	}).seal()
}
