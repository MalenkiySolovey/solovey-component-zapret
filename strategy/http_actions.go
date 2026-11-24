package strategy

import "strings"

// HTTPAction selects a pinned outbound-only header transform. The name is not
// a general Lua function reference, and the spelling cannot change the header.
type HTTPAction struct {
	Name  string `json:"name"`
	Spell string `json:"spell,omitempty"`
}

const (
	HTTPDomainCase = "http_domcase"
	HTTPHostCase   = "http_hostcase"
)

func parseHTTPAction(value string) (HTTPAction, bool) {
	name, param, present := strings.Cut(value, ":")
	action := HTTPAction{Name: name}
	if present {
		spell, ok := strings.CutPrefix(param, "spell=")
		if !ok || spell == "" {
			return HTTPAction{}, false
		}
		action.Spell = spell
	}
	return action, action.valid()
}

func (a HTTPAction) valid() bool {
	switch a.Name {
	case HTTPDomainCase:
		return a.Spell == ""
	case HTTPHostCase:
		// Reject non-ASCII case folds as well as arbitrary HTTP field names.
		return a.Spell == "" || len(a.Spell) == 4 && strings.EqualFold(a.Spell, "host")
	default:
		return false
	}
}

func validHTTPActions(actions []HTTPAction) bool {
	// Both transforms are idempotent: a repeated spelling simply overwrites
	// the earlier one. Each distinct transform incurs one bounded Lua call.
	if len(actions) == 0 || len(actions) > 2 {
		return false
	}
	for i, action := range actions {
		if !action.valid() || i > 0 && action.Name == actions[0].Name {
			return false
		}
	}
	return true
}

func (a HTTPAction) argument() string {
	flag := "--lua-desync=" + a.Name
	if a.Spell != "" {
		flag += ":spell=" + a.Spell
	}
	return flag
}
