package strategy

import (
	"fmt"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"
)

// NativeLists are owned by the Strategy revision. They contain data, not
// paths. Installed Hosts may materialize the same values as native files.
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

type nativeListFiles struct {
	directory string
	refs      map[string]int
	content   []string
}

func (f *nativeListFiles) appendArgs(args []string, lists *NativeLists) []string {
	if f == nil {
		return lists.appendArgs(args)
	}
	if lists == nil {
		return args
	}
	flags := [...]string{"--hostlist", "--hostlist-exclude", "--ipset", "--ipset-exclude"}
	for kind, list := range lists.fields() {
		if len(*list.entries) == 0 {
			continue
		}
		var text strings.Builder
		for _, entry := range *list.entries {
			text.WriteString(entry)
			text.WriteByte('\n')
		}
		data := text.String()
		index, exists := f.refs[data]
		if !exists {
			index = len(f.content)
			f.refs[data] = index
			f.content = append(f.content, data)
		}
		args = append(args, flags[kind]+"="+path.Join(f.directory, "list-"+strconv.Itoa(index)))
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
