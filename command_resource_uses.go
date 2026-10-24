package main

import "slices"

func (e Environment) setResource(decl ResourceDecl) {
	id := internEnvSTR(decl.GlobalVar)

	e.setVFS(id, decl.Value)

	if e.s.resourceBindings == nil {
		e.s.resourceBindings = make(map[ENV]STR)
	}

	e.s.resourceBindings[id] = decl.FetchKey
}

func (e Environment) recordResourceUse(id ENV) {
	if e.resourceUses != nil {
		if key, ok := e.s.resourceBindings[id]; ok && !slices.Contains(*e.resourceUses, key) {
			*e.resourceUses = append(*e.resourceUses, key)
		}
	}
}

func resourceKeyUnion(groups ...[]STR) []STR {
	var out []STR

	for _, group := range groups {
		for _, key := range group {
			if !slices.Contains(out, key) {
				out = append(out, key)
			}
		}
	}

	return out
}

func compilerValueResourceKeys(values []CompilerFlagValue) []STR {
	var out []STR

	for _, value := range values {
		out = resourceKeyUnion(out, value.Resources)
	}

	return out
}

func (e *StreamingEmitter) commandResources(n *Node) {
	var resources []STR

	for _, cmd := range n.Cmds {
		for _, key := range cmd.Resources {
			ref, found := n.resourceRef(key, e.fetchRefs)
			duplicate := slices.Contains(n.Resources, key) || slices.Contains(resources, key)

			if found && !duplicate {
				for _, old := range n.Resources {
					if other, ok := n.resourceRef(old, e.fetchRefs); ok && other == ref {
						duplicate = true

						break
					}
				}
			}

			if !duplicate {
				resources = append(resources, key)
			}
		}
	}

	if len(resources) > 0 {
		n.Resources = e.na.strs.list(append(append([]STR(nil), n.Resources...), resources...)...)
	}
}

func commonCompilerFlagValue(in ModuleCCInputs, p *Platform) CompilerFlagValue {
	value := compilerCommandFlags(in.CFlags, in.CompilerEnv)

	value.Args = concat(p.CFlags, value.Args, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal)
	value.Resources = resourceKeyUnion(value.Resources, compilerValueResourceKeys(in.CFlagValues))

	return value
}

func peerCompilerResourceKeys(values []CompilerFlagValue, hasOwn bool) []STR {
	if hasOwn {
		values = values[:len(values)-1]
	}

	return compilerValueResourceKeys(values)
}

func commandTokenValue(args []ANY, env Environment) CompilerFlagValue {
	var uses []STR
	env.resourceUses = &uses

	values := expandStmtTokens(args, env)

	return CompilerFlagValue{Args: values, Resources: uses}
}
