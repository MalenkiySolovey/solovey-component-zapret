package strategy

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// NativeLists are owned by the Strategy revision. They contain data, not
// paths; the compiler only emits the engine's inline list options.
type NativeLists struct {
	HostList        []string `json:"hostList,omitempty"`
	HostListExclude []string `json:"hostListExclude,omitempty"`
	IPSet           []string `json:"ipSet,omitempty"`
	IPSetExclude    []string `json:"ipSetExclude,omitempty"`
}

type nativeList struct {
	flag    string
	entries *[]string
	parse   func(string) (string, bool)
}

func (l *NativeLists) fields() [4]nativeList {
	return [4]nativeList{
		{"--hostlist-domains", &l.HostList, nativeDomain},
		{"--hostlist-exclude-domains", &l.HostListExclude, nativeDomain},
		{"--ipset-ip", &l.IPSet, nativeIPPrefix},
		{"--ipset-exclude-ip", &l.IPSetExclude, nativeIPPrefix},
	}
}

func (l *NativeLists) set(flag, input string) bool {
	for _, list := range l.fields() {
		if flag == list.flag {
			*list.entries = strings.Split(input, ",")
			return true
		}
	}
	return false
}

func (l NativeLists) seal() (*NativeLists, error) {
	hasEntries := false
	for _, list := range l.fields() {
		canonical := make([]string, 0, len(*list.entries))
		for _, raw := range *list.entries {
			value, ok := list.parse(raw)
			if !ok {
				return nil, fmt.Errorf("%w: %s has an invalid entry at position %d", ErrInvalidStrategy, list.flag, len(canonical)+1)
			}
			canonical = append(canonical, value)
		}
		slices.Sort(canonical)
		if len(canonical) == 0 {
			*list.entries = nil
		} else {
			*list.entries = slices.Compact(canonical)
			hasEntries = true
		}
	}
	if !hasEntries {
		return nil, nil
	}
	return &l, nil
}

func (l *NativeLists) appendArgs(args []string) []string {
	if l != nil {
		for _, list := range l.fields() {
			if len(*list.entries) != 0 {
				args = append(args, list.flag+"="+strings.Join(*list.entries, ","))
			}
		}
	}
	return args
}

func nativeDomain(value string) (string, bool) {
	if len(value) == 0 || len(value) > 253 {
		return "", false
	}
	for label := range strings.SplitSeq(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, ch := range label {
			if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return "", false
			}
		}
	}
	return strings.ToLower(value), true
}

func nativeIPPrefix(value string) (string, bool) {
	if prefix, err := netip.ParsePrefix(value); err == nil && !prefix.Addr().Is4In6() {
		return prefix.Masked().String(), true
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Zone() == "" {
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()).String(), true
	}
	return "", false
}
