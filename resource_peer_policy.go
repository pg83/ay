package main

import "strings"

func (d *ModuleData) captureResourcePeerPolicy(name string, env Environment) {
	if name != "PASS_PEERS" || d.moduleStmt == nil || d.resourcePeerClosed {
		return
	}

	d.resourcePeerExplicit = true
	d.resourcePeerOverride = internStr(env.string(internEnv(name)))
}

func (d *ModuleData) finishResourcePeerPolicy() {
	if d.resourcePeerClosed {
		return
	}

	d.resourcePeerClosed = true

	d.resourcePassPeers = (d.unit.Type == tokDynamicLibrary || d.unit.Tag == strYqlUdfStatic || !isLinkingModuleType(d.unit.Type)) &&
		d.unit.Type != tokDocs && d.unit.Type != tokProtoDescriptions && d.unit.Type != tokProtoRegistry &&
		d.unit.Type != tokExectest && d.unit.Type != tokPrebuiltProgram

	if d.resourcePeerExplicit {
		switch strings.ToLower(d.resourcePeerOverride.string()) {
		case "true", "t", "yes", "y", "on", "1", "da":
			d.resourcePassPeers = true
		default:
			d.resourcePassPeers = false
		}
	}
}
