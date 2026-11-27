package strategy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
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
	return parseNativeWithLua(id, text, nil)
}

func parseNativeWithLua(id, text string, managed []LuaReference) (Strategy, error) {
	tokens, err := nativeSource(text)
	if err != nil {
		return Strategy{}, err
	}
	type draft struct {
		Profile
		l7, inRange, outRange string
		outRangePending       bool
		seen                  map[string]bool
		actionOrder           []NativeActionRef
	}
	first := func(id string) draft {
		return draft{Profile: Profile{ID: id}, seen: map[string]bool{}}
	}
	current := first("profile-1")
	value := Strategy{Schema: Schema, ID: id, ManagedLua: managed}
	finish := func() error {
		if current.outRangePending {
			return fmt.Errorf("%w: --out-range needs a following action in native profile %d", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		if len(current.LuaActions) != 0 && current.hasNonManagedAction() {
			if current.Protocol == "" || current.inRange != "" {
				return fmt.Errorf("%w: managed Lua cannot be combined with an incoming-range or Circular return-path action", ErrInvalidStrategy)
			}
			current.Payload, current.L7Filter = "", current.l7
			current.ActionOrder = append([]NativeActionRef(nil), current.actionOrder...)
			value.Profiles = append(value.Profiles, current.Profile)
			return nil
		}
		if len(current.LuaActions) != 0 {
			if current.Protocol == "" || current.inRange != "" {
				return fmt.Errorf("%w: managed Lua requires an admitted outgoing TCP or UDP profile", ErrInvalidStrategy)
			}
			current.Payload, current.L7Filter = "", current.l7
			value.Profiles = append(value.Profiles, current.Profile)
			return nil
		}
		if current.Protocol == "" || !current.Pass && current.seen["--payload"] != current.seen["--lua-desync"] ||
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
	for _, token := range tokens {
		arg := token.text
		key, input, hasValue := strings.Cut(arg, "=")
		bad := token.invalid(fmt.Errorf("%w: unsupported, repeated, or malformed native option", ErrInvalidStrategy))
		if strings.ContainsFunc(arg, func(c rune) bool { return c < '!' || c > '~' }) {
			return Strategy{}, bad
		}
		if key == "--lua-init" {
			return Strategy{}, token.invalid(fmt.Errorf("%w: %w", ErrInvalidStrategy, ErrUnsupportedCustomLua))
		}
		if key == "--new" {
			if err := finish(); err != nil {
				return Strategy{}, token.invalid(err)
			}
			current = first(fmt.Sprintf("profile-%d", len(value.Profiles)+1))
			if hasValue {
				current.ID = input
				current.seen["--name"] = true
			}
			continue
		}
		repeatable := key == "--in-range" || key == "--out-range" || key == "--lua-desync" &&
			(current.Circular != nil || len(current.HTTPActions) != 0 || len(current.LuaActions) != 0 || len(current.actionOrder) != 0) ||
			key == "--payload" && len(current.actionOrder) != 0
		if !hasValue || input == "" || current.seen[key] && !repeatable {
			return Strategy{}, bad
		}
		current.seen[key] = true
		switch key {
		case "--name":
			current.ID = input
		case "--filter-l3":
			switch input {
			case "ipv4,ipv6", "ipv6,ipv4":
				// Explicitly matching both is identical to the engine default.
			case string(IPv4), string(IPv6):
				current.Family = AddressFamily(input)
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
			switch {
			case current.Protocol == "":
				current.Protocol, current.PortRanges = protocol, ports
			case protocol == intent.PacketProtocolTCP:
				current.Protocol, current.PortRanges, current.UDPRanges = protocol, ports, current.PortRanges
			default:
				current.UDPRanges = ports
			}
		case "--filter-l7":
			if !validNativeSelector(input, nativeL7Names) {
				return Strategy{}, bad
			}
			current.l7 = input
		case "--in-range":
			// The pinned engine snapshots the range at each Lua action.
			observe := current.inRange == "" && input == circularObserveRange() && !current.seen["--lua-desync"]
			disable := current.inRange == circularObserveRange() && input == circularVariantRange && current.Circular != nil
			if current.seen["--payload"] || !(observe || disable) {
				return Strategy{}, bad
			}
			current.inRange = input
		case "--out-range":
			if current.Circular != nil {
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
				if current.Circular != nil || current.inRange != "" {
					return Strategy{}, token.invalid(fmt.Errorf("%w: managed Lua cannot be inserted into Circular's return-path range", ErrInvalidStrategy))
				}
				name, arguments, _ := strings.Cut(input, ":")
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
				if current.inRange != circularVariantRange || !current.seen["--payload"] || !current.Circular.addVariant(input) {
					return Strategy{}, bad
				}
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
				if current.seen["--out-range"] {
					return Strategy{}, fmt.Errorf("%w: --out-range is not qualified for Circular's return-path action", ErrInvalidStrategy)
				}
				if current.inRange != circularObserveRange() || current.seen["--payload"] {
					return Strategy{}, fmt.Errorf("%w: Circular requires --in-range=%s before its action, then --in-range=%s and --payload before its variants",
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
			if current.Lists == nil {
				current.Lists = new(NativeLists)
			}
			if !current.Lists.set(key, input) {
				return Strategy{}, bad
			}
		}
	}
	if err := finish(); err != nil {
		return Strategy{}, err
	}
	value.Assets = nativeAssets(value.Profiles)
	return value, nil
}

func builtInLuaAction(input string) bool {
	if _, ok := parseHTTPAction(input); ok {
		return true
	}
	return input == "pass" || input == "circular" || strings.HasPrefix(input, "circular:fails=") ||
		strings.HasPrefix(input, "multisplit:pos=") || strings.HasPrefix(input, "fake:blob=")
}

func parseNativePorts(input string, protocol intent.PacketProtocol) ([]intent.PortInterval, error) {
	var ports []intent.PortInterval
	for field := range strings.SplitSeq(input, ",") {
		first, last, ranged := strings.Cut(field, "-")
		if !ranged {
			last = first
		}
		low, lowErr := strconv.ParseUint(first, 10, 16)
		high, highErr := strconv.ParseUint(last, 10, 16)
		if lowErr != nil || highErr != nil || low == 0 || high < low {
			return nil, fmt.Errorf("%w: %s filter requires positive port numbers or inclusive ranges", ErrInvalidStrategy, protocol)
		}
		ports = append(ports, intent.PortInterval{First: uint16(low), Last: uint16(high)})
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
			for _, arg := range profile.circularArgs("", false) {
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
