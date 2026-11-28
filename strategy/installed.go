package strategy

import (
	"fmt"
	"path"
	"strings"
)

// InstalledEngineInput binds argv to the exact ordered files the Host must
// materialize. nfqws2 canonicalizes list paths with realpath, so unlike the
// optional Lua bundle, its lists cannot be backed by anonymous memfds.
type InstalledEngineInput struct {
	Args      []string
	Trace     *NativeTrace
	Lua       string
	ListFiles []string
}

// CompileInstalledBindings keeps native list values out of argv. Only the
// privileged launcher can materialize these files; no Strategy can set a path.
// ListFiles maps to list-0, list-1, etc. in the Host-owned directory.
// A nil sources value selects built-in Lua only.
func CompileInstalledBindings(engineRoot, listDir, luaBundle string, queue uint16, loopMark uint32, bindings []MarkedStrategy,
	sources [][]LuaModule, trace, returnPath bool,
) (InstalledEngineInput, error) {
	if !path.IsAbs(listDir) || path.Clean(listDir) != listDir || listDir == "/" || strings.ContainsAny(listDir, "\x00\r\n") {
		return InstalledEngineInput{}, fmt.Errorf("%w: invalid installed list directory", ErrInvalidStrategy)
	}
	files := nativeListFiles{directory: listDir, refs: make(map[string]int)}
	var command InstalledEngineInput
	var err error
	if sources != nil {
		command.Args, command.Trace, command.Lua, err = compileManagedBindings(engineRoot, luaBundle, queue, loopMark, bindings,
			sources, trace, returnPath, &files)
	} else {
		budget, err := currentEngineArgBudget()
		if err != nil {
			return InstalledEngineInput{}, err
		}
		command.Args, command.Trace, err = compileBindingsWithLuaWithin(budget, engineRoot, queue, loopMark, bindings, trace, returnPath, nil, &files)
		if err != nil {
			return InstalledEngineInput{}, err
		}
	}
	if err != nil {
		return InstalledEngineInput{}, err
	}
	command.ListFiles = files.content
	return command, nil
}
