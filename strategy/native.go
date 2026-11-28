package strategy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

var ErrUnsupportedCustomLua = errors.New("UNSUPPORTED_CUSTOM_LUA: select managed Lua source revisions instead of loading a host file or inline Lua")

// ParseNative accepts native profile fields, not runnable engine argv.
// The caller bounds the input transport; activation budgets the compiled argv.
// The caller can retain text unchanged for a lossless raw round trip.
func ParseNative(id, text string) (Strategy, error) {
	value, err := parseNative(id, text)
	if err != nil {
		return Strategy{}, err
	}
	return Seal(value)
}

func parseNative(id, text string) (Strategy, error) {
	return parseNativeWithLua(id, text, nil, nil)
}

func parseNativeWithLua(id, text string, managed []LuaReference, lists *nativeListProjection) (Strategy, error) {
	tokens, err := nativeSource(text)
	if err != nil {
		return Strategy{}, err
	}
	first := func(id string) nativeDraft {
		return nativeDraft{Profile: Profile{ID: id}, seen: map[string]bool{}}
	}
	ordinal := 1
	current := first("profile-1")
	templates := map[string]nativeDraft{}
	value := Strategy{Schema: Schema, ID: id, ManagedLua: managed}
	finish := func() error {
		if current.skipped {
			if lists != nil {
				if !current.template {
					lists.addSkipped(current.ID)
				} else if current.declaredListRefs != (NativeListRefs{}) {
					return lists.addTemplate(current.ID, current.declaredListRefs)
				}
			}
			return nil
		}
		if current.outRangePending {
			return fmt.Errorf("%w: --out-range needs a following action in native profile %d", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		if current.template {
			if !current.named || !outbounds.ValidZapretStrategyRef(current.ID) {
				return fmt.Errorf("%w: name the template with --template=<id> or --name=<id>", ErrInvalidStrategy)
			}
			if _, exists := templates[current.ID]; exists {
				return fmt.Errorf("%w: template %q is already defined", ErrInvalidStrategy, current.ID)
			}
			if current.Lists != nil {
				lists, err := current.Lists.seal()
				if err != nil {
					return err
				}
				current.Lists = lists
			}
			if current.declaredListRefs != (NativeListRefs{}) {
				if err := lists.addTemplate(current.ID, current.declaredListRefs); err != nil {
					return err
				}
			}
			templates[current.ID] = current
			return nil
		}
		if current.declaredListRefs != (NativeListRefs{}) {
			return fmt.Errorf("%w: select managed lists for an active profile in the editor; lists/<id>.txt options belong to templates", ErrInvalidStrategy)
		}
		if current.Protocol == "" {
			return fmt.Errorf("%w: native profile %d requires a nonempty TCP or UDP destination port scope (1-65535)", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		if current.listRefs != (NativeListRefs{}) {
			lists.addActive(current.ID, current.listRefs)
		}
		if current.Circular != nil {
			if current.circularPayload == "" || payloadProtocol(current.circularPayload) != current.l7 {
				return fmt.Errorf("%w: Circular variants need the profile's matching TLS or HTTP payload filter", ErrInvalidStrategy)
			}
			current.Payload = current.circularPayload
			started := false
			for _, ref := range current.actionOrder {
				if ref.Kind == nativeActionCircular && ref.Index == 0 {
					started = true
				}
				if started && ref.Kind == nativeActionManaged {
					if tag, ok := circularStrategyTag(current.LuaActions[ref.Index].Arguments); !ok || tag > len(current.Circular.Splits) {
						return fmt.Errorf("%w: a callback after Circular must name a defined strategy in 1..%d", ErrInvalidStrategy, len(current.Circular.Splits))
					}
				}
			}
		}
		if len(current.LuaActions) != 0 && current.hasNonManagedAction() {
			if current.Circular == nil && current.inRange != "" {
				return fmt.Errorf("%w: managed Lua needs an outgoing-only action window", ErrInvalidStrategy)
			}
			current.Payload, current.L7Filter = "", current.l7
			current.ActionOrder = append([]NativeActionRef(nil), current.actionOrder...)
			value.Profiles = append(value.Profiles, current.Profile)
			return nil
		}
		if len(current.LuaActions) != 0 {
			if current.inRange != "" {
				return fmt.Errorf("%w: managed Lua requires an admitted outgoing TCP or UDP profile", ErrInvalidStrategy)
			}
			current.Payload, current.L7Filter = "", current.l7
			value.Profiles = append(value.Profiles, current.Profile)
			return nil
		}
		if !current.Pass && current.seen["--payload"] != current.seen["--lua-desync"] ||
			!current.Pass && current.hasAction() && (current.l7 == "" || payloadProtocol(current.Payload) != current.l7) ||
			current.inRange != "" && current.Circular == nil {
			return fmt.Errorf("%w: incomplete or inconsistent native profile %d", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		if current.Pass || !current.hasAction() {
			current.L7Filter = current.l7
		}
		value.Profiles = append(value.Profiles, current.Profile)
		return nil
	}
	var skippedUntil int
	for i, token := range tokens {
		if i < skippedUntil {
			continue
		}
		arg := token.text
		key, input, hasValue := strings.Cut(arg, "=")
		if key == "--new" {
			if err := finish(); err != nil {
				return Strategy{}, token.invalid(err)
			}
			if !current.template {
				ordinal++
			}
			current = first(fmt.Sprintf("profile-%d", ordinal))
			if hasValue {
				current.ID, current.named = input, true
				current.seen["--name"] = true
			}
		}
		if i == 0 || key == "--new" {
			end := nativeProfileEnd(tokens, i)
			if nativeProfileSkipped(tokens[i:end]) {
				if err := current.skipNativeProfile(tokens[i:end]); err != nil {
					return Strategy{}, err
				}
				skippedUntil = end
				continue
			}
		}
		bad := token.invalid(fmt.Errorf("%w: unsupported, repeated, or malformed native option", ErrInvalidStrategy))
		if strings.ContainsFunc(arg, func(c rune) bool { return c < '!' || c > '~' }) {
			return Strategy{}, bad
		}
		if key == "--new" {
			continue
		}
		if key == "--lua-init" {
			return Strategy{}, token.invalid(fmt.Errorf("%w: %w", ErrInvalidStrategy, ErrUnsupportedCustomLua))
		}
		if key == "--template" {
			if hasValue && !outbounds.ValidZapretStrategyRef(input) {
				return Strategy{}, bad
			}
			current.template = true
			if hasValue {
				current.ID, current.named = input, true
				current.seen["--name"] = true
			}
			continue
		}
		if key == "--import" {
			if !hasValue || !outbounds.ValidZapretStrategyRef(input) {
				return Strategy{}, token.invalid(fmt.Errorf("%w: specify a template ID after --import", ErrInvalidStrategy))
			}
			template, exists := templates[input]
			if !exists {
				return Strategy{}, token.invalid(fmt.Errorf("%w: template %q must be defined in an earlier profile", ErrInvalidStrategy, input))
			}
			if err := current.importTemplate(template); err != nil {
				return Strategy{}, token.invalid(err)
			}
			continue
		}
		if current.inheritedActions && (key == "--lua-desync" || key == "--payload" || key == "--in-range" || key == "--out-range") {
			return Strategy{}, token.invalid(errTemplateActionOrder)
		}
		repeatable := key == "--filter-l3" || key == "--in-range" || key == "--out-range" || key == "--lua-desync" &&
			(current.Circular != nil || len(current.HTTPActions) != 0 || len(current.LuaActions) != 0 || len(current.actionOrder) != 0) ||
			key == "--payload" && len(current.actionOrder) != 0 ||
			current.template && key == "--name"
		if !hasValue || input == "" || current.seen[key] && !repeatable {
			return Strategy{}, bad
		}
		current.seen[key] = true
		switch key {
		case "--name":
			current.ID, current.named = input, true
		case "--filter-l3":
			switch input {
			case "ipv4,ipv6", "ipv6,ipv4":
				current.addFamily("")
			case string(IPv4), string(IPv6):
				current.addFamily(AddressFamily(input))
			default:
				return Strategy{}, bad
			}
		case "--filter-tcp", "--filter-udp":
			protocol := intent.PacketProtocolTCP
			if key == "--filter-udp" {
				protocol = intent.PacketProtocolUDP
			}
			ports, err := parseNativePorts(input, protocol)
			if err != nil {
				return Strategy{}, token.invalid(err)
			}
			current.addPortFilters(protocol, ports)
		case "--filter-l7":
			if !validNativeSelector(input, nativeL7Names) {
				return Strategy{}, bad
			}
			current.l7 = input
		case "--in-range":
			// The pinned engine snapshots the range at each Lua action.
			if current.inRange == "" && input == circularVariantRange {
				continue // x is already the engine's initial incoming range.
			}
			observe := current.inRange == "" && input == circularObserveRange() && !current.hasNonManagedAction()
			disable := current.inRange == circularObserveRange() && input == circularVariantRange && current.Circular != nil
			if !(observe || disable) {
				return Strategy{}, bad
			}
			current.inRange = input
		case "--out-range":
			if current.inRange == circularObserveRange() && current.Circular != nil {
				return Strategy{}, fmt.Errorf("%w: --out-range is not qualified for Circular's return-path action", ErrInvalidStrategy)
			}
			if current.outRangePending {
				return Strategy{}, fmt.Errorf("%w: apply --out-range to an action before changing it", ErrInvalidStrategy)
			}
			current.outRange, current.outRangePending = input, true
		case "--payload":
			if current.Pass {
				return Strategy{}, token.invalid(fmt.Errorf("%w: put --payload before pass; its filter is captured when the action is added", ErrInvalidStrategy))
			}
			if current.Circular != nil && current.inRange != circularVariantRange {
				return Strategy{}, bad
			}
			current.Payload = Payload(input)
		case "--lua-desync":
			if len(managed) != 0 && !builtInLuaAction(input) {
				if current.inRange != "" && current.inRange != circularVariantRange {
					return Strategy{}, token.invalid(fmt.Errorf("%w: managed Lua cannot be inserted into Circular's return-path range", ErrInvalidStrategy))
				}
				name, arguments, _ := strings.Cut(input, ":")
				if current.Circular != nil {
					if _, ok := circularStrategyTag(arguments); !ok {
						return Strategy{}, token.invalid(fmt.Errorf("%w: a callback after Circular needs an explicit strategy=N for one of its variants", ErrInvalidStrategy))
					}
				}
				action := LuaAction{Function: name, Arguments: arguments, Payload: current.Payload, OutRange: current.outRange}
				if !action.valid() {
					return Strategy{}, bad
				}
				current.LuaActions = append(current.LuaActions, action)
				current.actionOrder = append(current.actionOrder, NativeActionRef{Kind: nativeActionManaged, Index: len(current.LuaActions) - 1})
				current.outRangePending = false
				continue
			}
			if current.Circular != nil {
				if current.inRange != circularVariantRange || (current.outRange != "" && current.outRange != "a") ||
					(current.Payload != TLSClientHello && current.Payload != HTTPRequest) ||
					(current.circularPayload != "" && current.circularPayload != current.Payload) || !current.Circular.addVariant(input) {
					return Strategy{}, bad
				}
				current.circularPayload = current.Payload
				current.actionOrder = append(current.actionOrder, NativeActionRef{Kind: nativeActionCircular, Index: len(current.Circular.Splits), Payload: current.Payload})
				current.outRangePending = false
				continue
			}
			if action, ok := parseHTTPAction(input); ok {
				if current.Payload != HTTPRequest || current.inRange != "" || len(current.HTTPActions) == 2 {
					return Strategy{}, bad
				}
				action.OutRange, current.outRangePending = current.outRange, false
				current.HTTPActions = append(current.HTTPActions, action)
				current.actionOrder = append(current.actionOrder, NativeActionRef{Kind: nativeActionHTTP, Index: len(current.HTTPActions) - 1})
				continue
			}
			if len(current.HTTPActions) != 0 {
				return Strategy{}, bad
			}
			if input == "pass" {
				if current.hasNonManagedAction() {
					return Strategy{}, bad
				}
				current.Pass = true
				current.OutRange, current.outRangePending = current.outRange, false
				current.actionOrder = append(current.actionOrder, NativeActionRef{Kind: nativeActionBuiltin, Payload: current.Payload})
				continue
			}
			if fails, ok := strings.CutPrefix(input, "circular:fails="); input == "circular" || ok {
				if current.outRange != "" && current.outRange != "a" {
					return Strategy{}, fmt.Errorf("%w: --out-range is not qualified for Circular's return-path action", ErrInvalidStrategy)
				}
				if current.inRange != circularObserveRange() || (current.Payload != "" && current.Payload != "all") {
					return Strategy{}, fmt.Errorf("%w: Circular requires --in-range=%s and an unrestricted payload before its action, then --in-range=%s and --payload before its variants",
						ErrInvalidStrategy, circularObserveRange(), circularVariantRange)
				}
				n := uint64(3)
				if ok {
					var err error
					n, err = strconv.ParseUint(fails, 10, 8)
					if err != nil {
						return Strategy{}, bad
					}
				}
				current.Circular = &Circular{Failures: uint8(n)}
				current.actionOrder = append(current.actionOrder, NativeActionRef{Kind: nativeActionCircular})
				current.outRangePending = false
				continue
			}
			if pos, ok := strings.CutPrefix(input, "multisplit:pos="); ok {
				n, err := strconv.ParseUint(pos, 10, 16)
				if err != nil || n == 0 {
					return Strategy{}, bad
				}
				current.SplitAt = uint16(n)
			} else if fake, ok := strings.CutPrefix(input, "fake:blob="); ok {
				blob, repeats, ok := strings.Cut(fake, ":repeats=")
				n, err := strconv.ParseUint(repeats, 10, 8)
				if !ok || err != nil || n == 0 {
					return Strategy{}, bad
				}
				if blob != defaultQUICBlobName {
					id, ok := strings.CutPrefix(blob, nativeBlobRefPrefix)
					if !ok || id == "" {
						return Strategy{}, bad
					}
					current.QUICBlobRef = id
				}
				current.QUICRepeats = uint8(n)
			} else {
				return Strategy{}, bad
			}
			current.actionOrder = append(current.actionOrder, NativeActionRef{Kind: nativeActionBuiltin, Payload: current.Payload})
			current.OutRange, current.outRangePending = current.outRange, false
		default:
			if slot := current.declaredListRefs.slot(key); slot != nil {
				id := presetAssetID(unquotePresetPath(input), "lists/", ".txt")
				if lists == nil || id == "" {
					return Strategy{}, token.invalid(fmt.Errorf("%w: templates can reference catalog lists as lists/<managed-id>.txt; host paths are never read", ErrInvalidStrategy))
				}
				*slot = id
				if err := current.listRefs.merge(current.declaredListRefs); err != nil {
					return Strategy{}, token.invalid(err)
				}
				continue
			}
			var lists NativeLists
			if !lists.set(key, input) {
				return Strategy{}, bad
			}
			current.Lists, err = mergeTemplateLists(current.Lists, &lists)
			if err != nil {
				return Strategy{}, token.invalid(err)
			}
		}
	}
	if err := finish(); err != nil {
		return Strategy{}, err
	}
	if len(value.Profiles) == 0 {
		return Strategy{}, fmt.Errorf("%w: templates do not run and skipped profiles are inactive. Add an active profile", ErrInvalidStrategy)
	}
	if lists != nil {
		for _, profile := range value.Profiles {
			if lists.skipped[profile.ID] {
				return Strategy{}, fmt.Errorf("%w: active and skipped profiles share ID %q", ErrInvalidStrategy, profile.ID)
			}
		}
	}
	value.Assets = nativeAssets(value.Profiles)
	return value, nil
}

func (d *nativeDraft) skipNativeProfile(tokens []sourceToken) error {
	d.skipped = true
	for _, token := range tokens {
		key, value, hasValue := strings.Cut(token.text, "=")
		if key == "--lua-init" {
			return token.invalid(fmt.Errorf("%w: %w", ErrInvalidStrategy, ErrUnsupportedCustomLua))
		}
		if !skippedNativeOption(key, hasValue) {
			return token.invalid(fmt.Errorf("%w: --skip applies only to native profile options", ErrInvalidStrategy))
		}
		if key == "--template" {
			d.template = true
		}
		if hasValue && (key == "--name" || key == "--template") {
			d.ID = value
		}
		if slot := d.declaredListRefs.slot(key); slot != nil {
			id := presetAssetID(unquotePresetPath(value), "lists/", ".txt")
			if *slot != "" && id != "" && *slot != id {
				return token.invalid(fmt.Errorf("%w: use one managed list per native filter slot", ErrInvalidStrategy))
			}
			if id != "" {
				*slot = id
			}
		}
	}
	return nil
}

func builtInLuaAction(input string) bool {
	if _, ok := parseHTTPAction(input); ok {
		return true
	}
	return input == "pass" || input == "circular" || strings.HasPrefix(input, "circular:fails=") ||
		strings.HasPrefix(input, "multisplit:pos=") || strings.HasPrefix(input, "fake:blob=")
}

func parseNativePorts(input string, protocol intent.PacketProtocol) ([]intent.PortInterval, error) {
	const maxPort = 1<<16 - 1
	var ports []intent.PortInterval
	for field := range strings.SplitSeq(input, ",") {
		if field == "*" {
			ports = append(ports, intent.PortInterval{First: 1, Last: maxPort})
			continue
		}
		field, negated := strings.CutPrefix(field, "~")
		first, last, ranged := strings.Cut(field, "-")
		if !ranged {
			last = first
		}
		low, lowErr := strconv.ParseUint(first, 10, 16)
		high, highErr := strconv.ParseUint(last, 10, 16)
		if lowErr != nil || highErr != nil || high < low {
			return nil, fmt.Errorf("%w: %s filter requires ports or inclusive ranges in 0-65535, optionally prefixed with ~, or *", ErrInvalidStrategy, protocol)
		}
		// nfqws2 ORs the filters and never matches port zero. A zero-only
		// term denies all even when written with a tilde.
		if high == 0 {
			continue
		}
		if negated {
			if low > 1 {
				ports = append(ports, intent.PortInterval{First: 1, Last: uint16(low - 1)})
			}
			if high < maxPort {
				ports = append(ports, intent.PortInterval{First: uint16(high + 1), Last: maxPort})
			}
			continue
		}
		ports = append(ports, intent.PortInterval{First: uint16(max(low, 1)), Last: uint16(high)})
	}
	return ports, nil
}

func nativePortList(ports []uint16, ranges []intent.PortInterval) string {
	if len(ranges) == 0 {
		ranges, _ = intent.CanonicalPortIntervals(ports)
	}
	var text strings.Builder
	for i, r := range ranges {
		if i != 0 {
			text.WriteByte(',')
		}
		text.WriteString(strconv.Itoa(int(r.First)))
		if r.Last != r.First {
			text.WriteByte('-')
			text.WriteString(strconv.Itoa(int(r.Last)))
		}
	}
	return text.String()
}

func (p Profile) appendPortFilters(args []string) []string {
	args = append(args, "--filter-"+string(p.Protocol)+"="+nativePortList(p.Ports, p.PortRanges))
	if hasPorts(p.UDPPorts, p.UDPRanges) {
		args = append(args, "--filter-udp="+nativePortList(p.UDPPorts, p.UDPRanges))
	}
	return args
}

// FormatNative is used only when a visual edit changes the parsed profiles.
// It omits all runtime-owned flags and asset paths.
func FormatNative(value Strategy) (string, error) {
	value, err := Seal(value)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for i, profile := range value.Profiles {
		nameFlag := "--name"
		if i > 0 {
			nameFlag = "--new"
		}
		fmt.Fprintf(&text, "%s=%s\n", nameFlag, profile.ID)
		for _, filter := range profile.appendPortFilters(nil) {
			fmt.Fprintln(&text, filter)
		}
		if profile.Family != "" {
			fmt.Fprintf(&text, "--filter-l3=%s\n", profile.Family)
		}
		for _, arg := range profile.Lists.appendArgs(nil) {
			fmt.Fprintln(&text, arg)
		}
		if profile.Circular != nil {
			var args []string
			if len(profile.ActionOrder) != 0 {
				args = profile.appendOrderedCircular(nil, nil, "", false)
			} else {
				args = profile.circularArgs("", false)
			}
			for _, arg := range args {
				fmt.Fprintln(&text, arg)
			}
			continue
		}
		if len(profile.ActionOrder) != 0 {
			var blob string
			if profile.QUICBlobRef != "" {
				blob = nativeBlobRefPrefix + profile.QUICBlobRef
			}
			for _, arg := range profile.appendOrderedActions(nil, blob, nil, "") {
				fmt.Fprintln(&text, arg)
			}
			continue
		}
		if len(profile.LuaActions) != 0 {
			for _, arg := range profile.appendManagedLuaActions(nil, nil, "") {
				fmt.Fprintln(&text, arg)
			}
			continue
		}
		var blob string
		if profile.QUICBlobRef != "" {
			blob = nativeBlobRefPrefix + profile.QUICBlobRef
		}
		for _, arg := range profile.appendActions(nil, blob, "") {
			fmt.Fprintln(&text, arg)
		}
	}
	return text.String(), nil
}
