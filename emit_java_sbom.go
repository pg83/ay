package main

func (b *javaNodeBuilder) emitModule(jm *JavaModule) {
	if jm.unit.kind != juContrib {
		add := func(res *ModuleEmitResult) {
			if res.SbomComponentRef != nil {
				b.input(*res.SbomComponentPath)
				b.deps = append(b.deps, *res.SbomComponentRef)
			}

			b.input(res.PeerSbomClosurePaths...)
			b.deps = append(b.deps, res.PeerSbomClosureRefs...)
		}

		for _, peer := range jm.unmanageable {
			if output := moduleOutputPath(peer.res); output != 0 {
				b.modulePeers = append(b.modulePeers, output)
			}

			add(peer.res)
		}

		for _, entry := range b.ctx.javaManagedPeersClosure(jm) {
			if entry.java != nil {
				entry.java.eachUnmanageable(func(peer javaForeignPeer) {
					if output := moduleOutputPath(peer.res); output != 0 {
						b.modulePeers = append(b.modulePeers, output)
					}

					add(peer.res)
				})
			} else {
				add(entry.foreign)
			}
		}
	}

	b.emitTo(jm.outRef)
}

func (b *javaNodeBuilder) javaSbom(jm *JavaModule, closure []JavaClosureEntry) {
	d := jm.data

	if !d.embedSbom {
		return
	}

	args := b.python("build/internal/scripts/link_java_sbom.py")

	b.script("build/internal/scripts/link_sbom.py")
	b.script(sbomGenScriptRel)

	if d.needSbomInfo {
		peers := []string{"build/platform/java/jdk/jdk" + d.jdkReal}

		if d.withKotlin {
			peers = append(peers, "build/platform/java/kotlin")
		}

		for _, peer := range peers {
			component := build(peer, "/toolchain.component.sbom")

			args = append(args, strToolchain2.any(), component.any())
			b.input(component)
			b.deps = append(b.deps, b.use(component))
		}
	}

	args = append(args, strModPath.any(), internStr(jm.dir).any(), strO.any(),
		build(jm.dir, "/cls/META-INF/SBOM.json").any(), strVcsInfo.any(), bldVcsJson.any())

	for _, entry := range closure {
		args = append(args, entry.path.any())
	}

	b.cmd(args, 0)
}
