package main

type ConfModuleInit struct {
	steps     []confInitStep
	inputs    []ENV
	originals []ENV
	outputs   []ENV
	memo      []confInitMemo
}

type confInitStep struct {
	variable ENV
	source   ENV
}

type confInitMemo struct {
	inputs      []ConfValue
	assignments []ConfAssignment
}

type ConfAssignment struct {
	key         ENV
	value       ConfValue
	clearInputs bool
}

func (c *ConfConditions) moduleInit(module *ConfModule) *ConfModuleInit {
	if module.init != nil {
		return module.init
	}

	plan := &ConfModuleInit{}

	if parent := c.module(module.parent, ""); parent != nil {
		plan.steps = append(plan.steps, c.moduleInit(parent).steps...)
	}

	plan.steps = append(plan.steps, confInitStep{variable: module.variable})

	switch module.variable.string() {
	case "env_BASE_UNIT":
		plan.steps = append(plan.steps, confInitStep{variable: envBisonFlexSetDefaults})
	case "env_LINK_UNIT", "envGO_PROGRAM":
		plan.steps = append(plan.steps, confInitStep{variable: envLinkerId, source: envDefaultLinkerId}, confInitStep{variable: envCommonLinkSettings})
	}

	written := make([]bool, envTable.count)
	read := make([]bool, envTable.count)
	original := make([]bool, envTable.count)

	readVar := func(key ENV) {
		if !written[key] && !read[key] {
			read[key] = true
			plan.inputs = append(plan.inputs, key)
		}
	}

	writeVar := func(key ENV) {
		if !written[key] {
			written[key] = true
			plan.outputs = append(plan.outputs, key)
		}
	}

	for _, step := range plan.steps {
		if step.source != 0 {
			readVar(step.source)
		}

		writeVar(step.variable)

		targets, _ := c.byVariable.get(step.variable)

		for _, target := range targets {
			writeVar(target)

			if !original[target] {
				original[target] = true
				plan.originals = append(plan.originals, target)
			}

			definition, _ := c.byTarget.get(target)

			for _, r := range definition.stmts {
				r.file.condVars(r.stmt.branch, readVar)
			}
		}
	}

	module.init = plan

	return plan
}

func (m confInitMemo) matches(plan *ConfModuleInit, env Environment, originals map[STR]STR) bool {
	for i, key := range plan.inputs {
		if m.inputs[i] != confEnvValue(env, key) {
			return false
		}
	}

	for i, key := range plan.originals {
		if m.inputs[len(plan.inputs)+i].value != originals[key.str()] {
			return false
		}
	}

	return true
}

func (p *Platform) initConfModule(env Environment, name STR, tag string, originals map[STR]STR) {
	c := p.conditions

	if c == nil {
		return
	}

	module := c.module(name, tag)

	if module == nil {
		return
	}

	plan := c.moduleInit(module)

	if env.resourceUses != nil {
		p.runConfModuleInit(plan, env, originals, nil)

		return
	}

	for _, memo := range plan.memo {
		if memo.matches(plan, env, originals) {
			for _, assignment := range memo.assignments {
				assignment.value.apply(env, assignment.key)

				if assignment.clearInputs && assignment.value.kind == envAbsent {
					delete(env.s.textInputs, assignment.key)

					env.s.clearResourceBinding(assignment.key)
				}
			}

			return
		}
	}

	n := len(plan.inputs) + len(plan.originals)
	inputs := c.values.alloc(n)

	for i, key := range plan.inputs {
		inputs[i] = confEnvValue(env, key)
	}

	for i, key := range plan.originals {
		inputs[len(plan.inputs)+i] = ConfValue{value: originals[key.str()]}
	}

	c.values.commit(n)

	assignments := c.assignments.alloc(len(plan.outputs))

	for i, key := range plan.outputs {
		assignments[i] = ConfAssignment{key: key}
	}

	p.runConfModuleInit(plan, env, originals, assignments[:len(plan.outputs)])
	c.assignments.commit(len(plan.outputs))
	plan.memo = append(plan.memo, confInitMemo{inputs: inputs[:n:n], assignments: assignments[:len(plan.outputs):len(plan.outputs)]})
}

func (p *Platform) runConfModuleInit(plan *ConfModuleInit, env Environment, originals map[STR]STR, recording []ConfAssignment) {
	for _, step := range plan.steps {
		if step.source != 0 {
			env.setString(step.variable, env.string(step.source))
		} else {
			env.setBool(step.variable, true)
		}

		if recording != nil {
			recordConfAssignment(recording, step.variable, env)
		}

		p.recalcConfInto(env, step.variable, originals, recording)
	}
}

func recordConfAssignment(recording []ConfAssignment, key ENV, env Environment) {
	for i := range recording {
		if recording[i].key == key {
			v := confEnvValue(env, key)

			recording[i].value = v
			recording[i].clearInputs = recording[i].clearInputs || v.kind != envAbsent

			return
		}
	}

	panic("conf initialization wrote an undeclared output")
}
