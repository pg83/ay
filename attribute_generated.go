package main

import "strings"

func overrideGeneratedModuleDir(e *BufferedEmitter) {
	if e == nil {
		return
	}

	overrideENSubmoduleModuleDir(e)

	if len(e.generatedFirstClaim) == 0 && len(e.generatedNodeClaim) == 0 {
		return
	}

	for i, node := range e.nodes {
		if !generatedOwnerAttributable(node) {
			continue
		}

		if len(node.Outputs) == 0 {
			continue
		}

		current := node.TargetProperties.ModuleDir

		selfOwned := false

		for _, out := range node.Outputs {
			if e.generatedFirstClaim[out].Dir == current {
				selfOwned = true

				break
			}
		}

		if claim := e.generatedNodeClaim[NodeRef(i)]; claim != "" && !selfOwned {
			if claim != current {
				node.TargetProperties.ModuleDir = claim
			}

			continue
		}

		var claim GenOwner

		for _, out := range node.Outputs {
			c, ok := e.generatedFirstClaim[out]

			if !ok {
				continue
			}

			if claim.Dir == "" {
				claim = c

				continue
			}

			if c.Dir != claim.Dir {
				claim = GenOwner{}

				break
			}
		}

		if claim.Dir == "" || claim.Dir == current {
			continue
		}

		rewritePRBindirCwd(node, current, claim.Dir)

		node.TargetProperties.ModuleDir = claim.Dir

		if claim.Tag != 0 {
			node.TargetProperties.ModuleTag = claim.Tag
		}
	}
}

func generatedOwnerAttributable(node *Node) bool {
	switch node.KV.P {
	case pkPR, pkCF, pkCP, pkPY, pkSC:
		return true
	case pkAR:
		return node.TargetProperties.ModuleType == mtNone &&
			node.TargetProperties.ModuleLang == mlNone &&
			len(node.Outputs) == 1 &&
			!strings.HasSuffix(node.Outputs[0].rel(), ".a")
	default:
		return false
	}
}

func rewritePRBindirCwd(node *Node, from, to string) {
	if node.KV.P != pkPR || from == to {
		return
	}

	fromBin := build(from).string()

	for j := range node.Cmds {
		cwd := node.Cmds[j].Cwd

		if cwd == 0 {
			continue
		}

		s := cwd.string()

		toBin := build(to).string()

		switch {
		case s == fromBin:
			node.Cmds[j].Cwd = internStr(toBin)
		case strings.HasPrefix(s, fromBin+"/"):
			node.Cmds[j].Cwd = internStr(toBin + s[len(fromBin):])
		}
	}
}

func overrideENSubmoduleModuleDir(e *BufferedEmitter) {
	if len(e.generatedENIncluderDirs) == 0 {
		return
	}

	moduleDirs := make(map[string]struct{}, len(e.nodes))

	for _, node := range e.nodes {
		if md := node.TargetProperties.ModuleDir; md != "" {
			moduleDirs[md] = struct{}{}
		}
	}

	nearestModuleDir := func(dir string) string {
		for d := dir; d != ""; d = pathDir(d) {
			if _, ok := moduleDirs[d]; ok {
				return d
			}
		}

		return ""
	}

	for _, node := range e.nodes {
		if node.KV.P != pkEN || len(node.Outputs) == 0 {
			continue
		}

		declaring := node.TargetProperties.ModuleDir
		prefix := declaring + "/"

		var best string

		for _, out := range node.Outputs {
			for _, incDir := range e.generatedENIncluderDirs[out] {
				m := nearestModuleDir(incDir)

				if m == "" || !strings.HasPrefix(m, prefix) {
					continue
				}

				if len(m) > len(best) {
					best = m
				}
			}
		}

		if best != "" {
			node.TargetProperties.ModuleDir = best
		}
	}
}
