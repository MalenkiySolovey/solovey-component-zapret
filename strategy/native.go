package strategy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

const MaxNativeBytes = 8 << 10

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
		return Strategy{}, fmt.Errorf("%w: native text exceeds %d bytes", ErrInvalidStrategy, MaxNativeBytes)
	}
	for _, c := range text {
		if !(c >= ' ' && c <= '~' || c == '\n' || c == '\r' || c == '\t') {
			return Strategy{}, fmt.Errorf("%w: expected plain native options", ErrInvalidStrategy)
		}
	}
	type draft struct {
		Profile
		l7   string
		seen map[string]bool
	}
	first := func(id string) draft {
		return draft{Profile: Profile{ID: id}, seen: map[string]bool{}}
	}
	current := first("profile-1")
	value := Strategy{Schema: Schema, ID: id}
	finish := func() error {
		if current.Protocol == "" || current.seen["--payload"] != current.seen["--lua-desync"] ||
			current.seen["--filter-l7"] != current.hasAction() ||
			current.hasAction() && payloadProtocol(current.Payload) != current.l7 {
			return fmt.Errorf("%w: incomplete or inconsistent native profile %d", ErrInvalidStrategy, len(value.Profiles)+1)
		}
		if current.hasAction() {
			value.Assets = []AssetRef{Library, Actions}
		}
		value.Profiles = append(value.Profiles, current.Profile)
		return nil
	}
	for i, arg := range strings.Fields(text) {
		key, input, hasValue := strings.Cut(arg, "=")
		if key == "--new" {
			if err := finish(); err != nil {
				return Strategy{}, err
			}
			if len(value.Profiles) >= 16 {
				return Strategy{}, fmt.Errorf("%w: too many native profiles", ErrInvalidStrategy)
			}
			current = first(fmt.Sprintf("profile-%d", len(value.Profiles)+1))
			if hasValue {
				current.ID = input
				current.seen["--name"] = true
			}
			continue
		}
		bad := fmt.Errorf("%w: unsupported or malformed native option %d", ErrInvalidStrategy, i+1)
		if !hasValue || input == "" || current.seen[key] {
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
			for _, port := range strings.Split(input, ",") {
				n, err := strconv.ParseUint(port, 10, 16)
				if err != nil || n == 0 || len(current.Ports) >= 32 {
					return Strategy{}, bad
				}
				current.Ports = append(current.Ports, uint16(n))
			}
		case "--filter-l7":
			if input != "tls" && input != "http" && input != "quic" {
				return Strategy{}, bad
			}
			current.l7 = input
		case "--payload":
			current.Payload = Payload(input)
		case "--lua-desync":
			if input == "circular" || strings.HasPrefix(input, "circular:") {
				return Strategy{}, fmt.Errorf("native Circular is unavailable: %w", errReturnObservationUnavailable)
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
	return value, nil
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
		if value.NeedsReturn {
			return "", errReturnObservationUnavailable
		}
		nameFlag := "--name"
		if i > 0 {
			nameFlag = "--new"
		}
		fmt.Fprintf(&text, "%s=%s\n--filter-%s=", nameFlag, profile.ID, profile.Protocol)
		for j, port := range profile.Ports {
			if j > 0 {
				text.WriteByte(',')
			}
			fmt.Fprint(&text, port)
		}
		text.WriteByte('\n')
		if profile.Family != "" {
			fmt.Fprintf(&text, "--filter-l3=%s\n", profile.Family)
		}
		for _, arg := range profile.Lists.appendArgs(nil) {
			fmt.Fprintln(&text, arg)
		}
		var blob string
		if profile.QUICBlobRef != "" {
			blob = nativeBlobRefPrefix + profile.QUICBlobRef
		}
		for _, arg := range profile.actionArgs(blob) {
			fmt.Fprintln(&text, arg)
		}
	}
	return text.String(), nil
}
