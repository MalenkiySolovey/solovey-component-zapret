package strategy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

// Leave room in the 32 KiB transfer request for JSON escaping, the sealed
// definition, and asset references. Runtime argv is budgeted separately.
const MaxNativeBytes = 8 << 10
const minimumNativeProfileBytes = len("--filter-tcp=1")

var ErrUnsupportedCustomLua = errors.New("UNSUPPORTED_CUSTOM_LUA: custom Lua is not admitted; use a qualified built-in action")

func nativeTextBudgetError(used int) error {
	return fmt.Errorf("%w: native text requires at least %d bytes; limit is %d bytes", ErrInvalidStrategy, used, MaxNativeBytes)
}

func portScopeBudgetError(protocol intent.PacketProtocol, used int) error {
	return fmt.Errorf("%w: %s scope needs %d explicit ports; the Host admits at most %d per protocol", ErrInvalidStrategy, protocol, used, maxAdmittedPorts)
}

// ParseNative accepts only bounded native profile fields. Raw text is an
// editor input, never runnable engine argv.
// The caller can retain text unchanged for a lossless raw round trip.
func ParseNative(id, text string) (Strategy, error) {
	value, err := parseNative(id, text)
	if err != nil {
		return Strategy{}, err
	}
	return Seal(value)
}

func parseNative(id, text string) (Strategy, error) {
	if len(text) > MaxNativeBytes {
		return Strategy{}, nativeTextBudgetError(len(text))
	}
	for _, c := range text {
		if !(c >= ' ' && c <= '~' || c == '\n' || c == '\r' || c == '\t') {
			return Strategy{}, fmt.Errorf("%w: expected plain native options", ErrInvalidStrategy)
		}
	}
	type draft struct {
		Profile
		l7, inRange, outRange string
		outRangePending       bool
		seen                  map[string]bool
	}
	first := func(id string) draft {
		return draft{Profile: Profile{ID: id}, seen: map[string]bool{}}
	}
	current := first("profile-1")
	value := Strategy{Schema: Schema, ID: id}
	finish := func() error {
		if current.outRangePending {
			return fmt.Errorf("%w: --out-range needs a following action in native profile %d", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		if current.Protocol == "" || current.seen["--payload"] != current.seen["--lua-desync"] ||
			current.seen["--filter-l7"] != current.hasAction() ||
			current.hasAction() && payloadProtocol(current.Payload) != current.l7 ||
			current.inRange != "" && current.Circular == nil {
			return fmt.Errorf("%w: incomplete or inconsistent native profile %d", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		value.Profiles = append(value.Profiles, current.Profile)
		return nil
	}
	for i, arg := range strings.Fields(text) {
		key, input, hasValue := strings.Cut(arg, "=")
		if key == "--lua-init" {
			return Strategy{}, fmt.Errorf("%w: %w", ErrInvalidStrategy, ErrUnsupportedCustomLua)
		}
		if key == "--new" {
			if err := finish(); err != nil {
				return Strategy{}, err
			}
			current = first(fmt.Sprintf("profile-%d", len(value.Profiles)+1))
			if hasValue {
				current.ID = input
				current.seen["--name"] = true
			}
			continue
		}
		bad := fmt.Errorf("%w: unsupported or malformed native option %d", ErrInvalidStrategy, i+1)
		repeatable := key == "--in-range" || key == "--out-range" || key == "--lua-desync" && (current.Circular != nil || len(current.HTTPActions) != 0)
		if !hasValue || input == "" || current.seen[key] && !repeatable {
			return Strategy{}, bad
		}
		current.seen[key] = true
		switch key {
		case "--name":
			current.ID = input
		case "--filter-l3":
			current.Family = AddressFamily(input)
		case "--filter-tcp", "--filter-udp":
			if current.Protocol != "" {
				return Strategy{}, bad
			}
			current.Protocol = intent.PacketProtocolTCP
			if key == "--filter-udp" {
				current.Protocol = intent.PacketProtocolUDP
			}
			ports, err := parseNativePorts(input, current.Protocol)
			if err != nil {
				return Strategy{}, err
			}
			current.Ports = ports
		case "--filter-l7":
			if input != "tls" && input != "http" && input != "quic" {
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
			if current.Circular != nil && current.inRange != circularVariantRange {
				return Strategy{}, bad
			}
			current.Payload = Payload(input)
		case "--lua-desync":
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
				continue
			}
			if len(current.HTTPActions) != 0 {
				return Strategy{}, bad
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

func parseNativePorts(input string, protocol intent.PacketProtocol) ([]uint16, error) {
	var ports []uint16
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
		if needed := len(ports) + int(high-low) + 1; needed > maxAdmittedPorts {
			return nil, portScopeBudgetError(protocol, needed)
		}
		for port := low; port <= high; port++ {
			ports = append(ports, uint16(port))
		}
	}
	return ports, nil
}

func nativePortList(ports []uint16) string {
	var text strings.Builder
	for i := 0; i < len(ports); {
		if i != 0 {
			text.WriteByte(',')
		}
		end := i + 1
		for end < len(ports) && int(ports[end]) == int(ports[end-1])+1 {
			end++
		}
		text.WriteString(strconv.Itoa(int(ports[i])))
		if end > i+1 {
			text.WriteByte('-')
			text.WriteString(strconv.Itoa(int(ports[end-1])))
		}
		i = end
	}
	return text.String()
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
		fmt.Fprintf(&text, "%s=%s\n--filter-%s=%s\n", nameFlag, profile.ID, profile.Protocol, nativePortList(profile.Ports))
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
		var blob string
		if profile.QUICBlobRef != "" {
			blob = nativeBlobRefPrefix + profile.QUICBlobRef
		}
		for _, arg := range profile.appendActions(nil, blob, "") {
			fmt.Fprintln(&text, arg)
		}
	}
	if text.Len() > MaxNativeBytes {
		return "", nativeTextBudgetError(text.Len())
	}
	return text.String(), nil
}
