package strategy

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"regexp"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

//go:embed managed_lua.lua
var managedLuaLoader string

var luaIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var luaRevision = regexp.MustCompile(`^[a-f0-9]{64}$`)

// LuaAction uses the original --lua-desync function and argument syntax.
type LuaAction struct {
	Function  string  `json:"function"`
	Arguments string  `json:"arguments,omitempty"`
	Payload   Payload `json:"payload,omitempty"`
	OutRange  string  `json:"outRange,omitempty"`
}

func (a LuaAction) valid() bool {
	return luaIdentifier.MatchString(a.Function) && !strings.ContainsFunc(a.Arguments, func(c rune) bool { return c < '!' || c > '~' }) &&
		(a.Payload == "" || validNativeSelector(string(a.Payload), nativePayloadNames))
}

func (a LuaAction) argument(name string) string {
	if a.Arguments == "" {
		return "--lua-desync=" + name
	}
	return "--lua-desync=" + name + ":" + a.Arguments
}

func (p Profile) appendManagedLuaActions(args []string, entries []string, observation string) []string {
	if p.L7Filter != "" {
		args = append(args, "--filter-l7="+p.L7Filter)
	}
	var payload Payload
	var span string
	for i, action := range p.LuaActions {
		if action.Payload != payload {
			filter := action.Payload
			if filter == "" {
				filter = "all"
			}
			args = append(args, "--payload="+string(filter))
		}
		args = appendOutRange(args, span, action.OutRange)
		name := action.Function
		if entries != nil {
			name = entries[i]
		}
		if i == 0 && observation != "" {
			args = append(args, observation)
		}
		args = append(args, action.argument(name))
		payload, span = action.Payload, action.OutRange
	}
	return args
}

// LuaReference selects a specific revision. Approval is deliberately not a
// property of a draft, so editing or importing source cannot authorize it.
type LuaReference struct {
	ID              string `json:"id"`
	Revision        string `json:"revision"`
	ApproveImported bool   `json:"approveImported,omitempty"`
}

func (ref LuaReference) key() string { return ref.ID + "/" + ref.Revision }

// Revision covers both the source and its provenance.
func (d LuaDraft) Revision() string {
	data, _ := json.Marshal(d)
	return LuaSourceDigest(string(data))
}

func validLuaReferences(refs []LuaReference) bool {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !outbounds.ValidZapretStrategyRef(ref.ID) || !luaRevision.MatchString(ref.Revision) || seen[ref.ID] {
			return false
		}
		seen[ref.ID] = true
	}
	return true
}

// SelectedLua materializes only the source snapshots declared by this strategy.
func (c Catalog) SelectedLua(refs []LuaReference) ([]LuaModule, error) {
	return c.resolveLua(refs, true)
}

func (c Catalog) resolveLua(refs []LuaReference, requireApproval bool) ([]LuaModule, error) {
	if !validLuaReferences(refs) {
		return nil, fmt.Errorf("%w: invalid managed Lua selection", ErrInvalidStrategy)
	}
	modules := make([]LuaModule, 0, len(refs))
	for _, ref := range refs {
		draft, exists := c.LuaDrafts[ref.ID]
		if !exists || draft.Revision() != ref.Revision {
			draft, exists = c.LuaRevisions[ref.key()]
		}
		if !exists || draft.Revision() != ref.Revision || draft.Source == "" {
			return nil, fmt.Errorf("%w: Lua source %q at the selected revision is unavailable", ErrInvalidStrategy, ref.ID)
		}
		if err := draft.validate(ref.ID); err != nil {
			return nil, err
		}
		if requireApproval && draft.RequiresApproval() && !ref.ApproveImported {
			return nil, fmt.Errorf("%w: explicitly authorize the selected revision of imported Lua source %q", ErrInvalidStrategy, ref.ID)
		}
		module := LuaModule{ID: ref.ID, Draft: draft, Digest: LuaSourceDigest(draft.Source)}
		if ref.ApproveImported {
			module.ApprovedDigest = module.Digest
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func (c Catalog) retainLuaRevision(id string) Catalog {
	if old, ok := c.LuaDrafts[id]; ok {
		for _, record := range c.Records {
			for _, ref := range record.Definition.ManagedLua {
				if ref.ID == id && ref.Revision == old.Revision() {
					c.LuaRevisions = maps.Clone(c.LuaRevisions)
					if c.LuaRevisions == nil {
						c.LuaRevisions = make(map[string]LuaDraft)
					}
					c.LuaRevisions[ref.key()] = old
					return c
				}
			}
		}
	}
	return c
}

// PruneLuaRevisions removes rollback snapshots not referenced by a strategy.
// The ordinary change transaction and backup still retain previous catalogs.
func (c Catalog) PruneLuaRevisions() Catalog {
	if len(c.LuaRevisions) == 0 {
		return c
	}
	retained := map[string]LuaDraft{}
	for _, record := range c.Records {
		for _, ref := range record.Definition.ManagedLua {
			if draft, ok := c.LuaRevisions[ref.key()]; ok && c.LuaDrafts[ref.ID].Revision() != ref.Revision {
				retained[ref.key()] = draft
			}
		}
	}
	c.LuaRevisions = retained
	return c
}

type luaBindings struct {
	file    string
	entries [][]string
}

// CompileManagedBindings returns planner-scoped engine arguments plus the exact
// script the Host must stage at bundleFile. The file path is not an editor field.
func CompileManagedBindings(engineRoot, bundleFile string, queue uint16, loopMark uint32, bindings []MarkedStrategy,
	sources [][]LuaModule, trace, returnPath bool,
) ([]string, *NativeTrace, string, error) {
	return compileManagedBindings(engineRoot, bundleFile, queue, loopMark, bindings, sources, trace, returnPath, nil)
}

func compileManagedBindings(engineRoot, bundleFile string, queue uint16, loopMark uint32, bindings []MarkedStrategy,
	sources [][]LuaModule, trace, returnPath bool, lists *nativeListFiles,
) ([]string, *NativeTrace, string, error) {
	if len(sources) != len(bindings) || !path.IsAbs(bundleFile) || path.Clean(bundleFile) != bundleFile ||
		strings.ContainsAny(bundleFile, "\x00\r\n") {
		return nil, nil, "", fmt.Errorf("%w: managed Lua requires a broker-owned bundle file and exact sources", ErrInvalidStrategy)
	}
	prepared := luaBindings{file: bundleFile, entries: make([][]string, len(bindings))}
	var program strings.Builder
	for i, binding := range bindings {
		refs := binding.Strategy.ManagedLua
		if len(refs) != len(sources[i]) {
			return nil, nil, "", fmt.Errorf("%w: incomplete managed Lua dependencies", ErrInvalidStrategy)
		}
		if len(refs) == 0 {
			continue
		}
		for j, ref := range refs {
			module := sources[i][j]
			if module.ID != ref.ID || module.Draft.Revision() != ref.Revision ||
				(module.Draft.RequiresApproval() && !ref.ApproveImported) {
				return nil, nil, "", fmt.Errorf("%w: managed Lua source %q differs from the authorized strategy revision", ErrInvalidStrategy, ref.ID)
			}
		}
		var names []string
		for _, profile := range binding.Strategy.Profiles {
			for _, action := range profile.LuaActions {
				names = append(names, action.Function)
			}
		}
		bundle, entries, err := CompileManagedLua(i, sources[i], names)
		if err != nil {
			return nil, nil, "", err
		}
		prepared.entries[i] = entries
		program.WriteString("do\n")
		program.WriteString(bundle)
		program.WriteString("end\n")
	}
	if program.Len() == 0 {
		return nil, nil, "", fmt.Errorf("%w: no managed Lua actions to prepare", ErrInvalidStrategy)
	}
	budget, err := currentEngineArgBudget()
	if err != nil {
		return nil, nil, "", err
	}
	argv, observations, err := compileBindingsWithLuaWithin(budget, engineRoot, queue, loopMark, bindings, trace, returnPath, &prepared, lists)
	if err != nil {
		return nil, nil, "", err
	}
	return argv, observations, program.String(), nil
}

// LuaModule is a selected, revision-bound source from the managed catalog.
// Approval of imported code is scoped to the exact source digest.
type LuaModule struct {
	ID             string
	Draft          LuaDraft
	Digest         string
	ApprovedDigest string
}

// CompileManagedLua prepares a file for the original Lua 5.4 engine. The
// caller must stage it as a broker-owned file and enable it only under the
// Host's explicit Lua delegation. It never takes a host filename from a draft.
// The returned entry names are private to the supplied binding, in action order.
func CompileManagedLua(binding int, modules []LuaModule, functions []string) (string, []string, error) {
	if binding < 0 || len(modules) == 0 || len(functions) == 0 {
		return "", nil, fmt.Errorf("%w: managed Lua requires selected sources and actions", ErrInvalidStrategy)
	}
	var out strings.Builder
	out.WriteString(managedLuaLoader)
	seen := make(map[string]bool, len(modules))
	for _, module := range modules {
		if err := module.Draft.validate(module.ID); err != nil {
			return "", nil, err
		}
		if seen[module.ID] || module.Draft.Source == "" || module.Digest != LuaSourceDigest(module.Draft.Source) {
			return "", nil, fmt.Errorf("%w: managed Lua source %q is missing, duplicated or does not match its selected digest", ErrInvalidStrategy, module.ID)
		}
		if module.Draft.RequiresApproval() && module.ApprovedDigest != module.Digest {
			return "", nil, fmt.Errorf("%w: explicitly authorize the selected revision of imported Lua source %q", ErrInvalidStrategy, module.ID)
		}
		seen[module.ID] = true
		out.WriteString("solovey_load(")
		writeLuaString(&out, module.ID)
		out.WriteByte(',')
		writeLuaString(&out, module.Draft.Source)
		out.WriteString(")\n")
	}
	entries := make([]string, len(functions))
	for i, name := range functions {
		if !luaIdentifier.MatchString(name) {
			return "", nil, fmt.Errorf("%w: invalid managed Lua function identifier %q", ErrInvalidStrategy, name)
		}
		entry := fmt.Sprintf("solovey_managed_%d_%d", binding, i)
		entries[i] = entry
		fmt.Fprintf(&out, "solovey_bind(%q,%q)\n", entry, name)
	}
	return out.String(), entries, nil
}

func LuaSourceDigest(source string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
}

// Lua decimal escapes preserve the selected source bytes, including newlines
// and UTF-8, without making any part of that source syntax in the loader.
func writeLuaString(out *strings.Builder, text string) {
	out.WriteByte('"')
	for i := range len(text) {
		b := text[i]
		switch {
		case b == '"' || b == '\\':
			out.WriteByte('\\')
			out.WriteByte(b)
		case b < 0x20 || b > 0x7e:
			fmt.Fprintf(out, `\%03d`, b)
		default:
			out.WriteByte(b)
		}
	}
	out.WriteByte('"')
}
