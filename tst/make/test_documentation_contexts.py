from tst.make.lib import MakeTest, PROJECT


class DocumentationContextsTest(MakeTest):
    def test_explicit_sources_and_copy(self):
        self.write("ya.make", f"DOCS(answer)\nDOCS_CONFIG(config.yml)\nDOCS_DIR({PROJECT}/pages)\nDOCS_INCLUDE_SOURCES({PROJECT}/extra.txt)\nDOCS_COPY_FILES(value.txt FROM ${{ARCADIA_ROOT}}/{PROJECT}/assets NAMESPACE images)\nDOCS_VARS(title=Ответ emoji=🙂 html=<tag>)\nUSE_PLANTUML()\nEND()\n")
        self.write("config.yml", "title: Answer\n")
        self.write("pages/toc.yaml", "title: Answer\nitems:\n  - name: Answer\n    href: index.md\n")
        self.write("pages/index.md", "# {{ title }}\n")
        self.write("assets/value.txt", "42\n")
        self.write("extra.txt", "extra\n")
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/config.yml", self.node(graph, "answer.tar.gz")["inputs"])
        self.assertTrue(any(f"$(S)/{PROJECT}/assets/value.txt" in node["inputs"] for node in graph["graph"]))

    def test_library_explicit_source_root(self):
        self.write("ya.make", "DOCS_LIBRARY(answer)\nSRCS(SRCDIR pages **/a?.md EXCLUDE **/ignored/**)\nEND()\n")
        self.write("pages/nested/a1.md", "# Answer\n")
        self.write("pages/ignored/a2.md", "# Ignored\n")
        inputs = self.node(self.graph(), "answer.docslib")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/pages/nested/a1.md", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/pages/ignored/a2.md", inputs)

    def _cmd_args(self, node):
        return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]

    def test_proto_peers_feed_preprocessed_docs(self):
        self.write("ya.make", "RECURSE(doc)\n")
        self.write("doc/ya.make", f"DOCS()\nPEERDIR({PROJECT}/hub)\nEND()\n")
        self.write("doc/.yfm", "title: Answer\n")
        self.write("doc/index.md", "# Answer\n")
        self.write("common/ya.make", "PROTO_LIBRARY()\nINCLUDE_TAGS(DOCS_PROTO)\nSRCS(common.proto)\nEND()\n")
        self.write("common/common.proto", 'syntax = "proto3";\nmessage Common {}\n')
        self.write("hub/ya.make", f"PROTO_LIBRARY()\nINCLUDE_TAGS(DOCS_PROTO)\nUSE_COMMON_GOOGLE_APIS(api/annotations)\nSRCS(hub.proto)\nPEERDIR({PROJECT}/common)\nEND()\n")
        self.write("hub/hub.proto", f'syntax = "proto3";\nimport "google/api/annotations.proto";\nimport "{PROJECT}/common/common.proto";\nmessage Hub {{ Common common = 1; }}\n')
        graph = self.graph()
        args = self._cmd_args(self.node(graph, "doc/_preprocessed.tar.gz"))
        libs = [arg.rsplit("/", 1)[0] for arg in args if arg.endswith((".docslib", ".docslib.fake"))]
        self.assertEqual(libs, ["contrib/libs/googleapis-common-protos", f"{PROJECT}/common", f"{PROJECT}/hub", f"{PROJECT}/doc"])
        self.assertTrue(any("$(B)/contrib/libs/googleapis-common-protos/google/api/annotations.md" in node["outputs"] for node in graph["graph"]))

    def test_copy_namespace_is_kept_verbatim(self):
        self.write("ya.make", f"DOCS()\nDOCS_COPY_FILES(FROM ${{ARCADIA_ROOT}}/{PROJECT}/assets NAMESPACE nested/dir/ a.txt)\nDOCS_COPY_FILES(FROM ${{ARCADIA_ROOT}}/{PROJECT}/assets b.txt)\nEND()\n")
        self.write(".yfm", "title: Answer\n")
        self.write("assets/a.txt", "a\n")
        self.write("assets/b.txt", "b\n")
        graph = self.graph()
        for output, dst in (("nested/dir/a.txt", f"$(B)/{PROJECT}/nested/dir/"), ("b.txt", f"$(B)/{PROJECT}/.")):
            with self.subTest(output=output):
                args = self._cmd_args(self.node(graph, output))
                self.assertEqual(args[args.index("--dst-dir") + 1], dst)

    def test_generated_files_follow_statement_order(self):
        self.write("ya.make", f"DOCS()\nDOCS_COPY_FILES(FROM ${{ARCADIA_ROOT}}/{PROJECT}/assets NAMESPACE first a.txt)\nRUN_PYTHON3(generate.py STDOUT generated.txt)\nDOCS_COPY_FILES(FROM ${{ARCADIA_ROOT}}/{PROJECT}/assets NAMESPACE second b.txt)\nEND()\n")
        self.write(".yfm", "title: Answer\n")
        self.write("generate.py", "print(42)\n")
        self.write("assets/a.txt", "a\n")
        self.write("assets/b.txt", "b\n")
        args = self._cmd_args(self.node(self.graph(), "__docs_internal.docslib.fake"))
        bindir = args.index("--bin-dir")
        generated = args[bindir + 3:args.index("--dest-dir")]
        self.assertEqual(generated, [f"$(B)/{PROJECT}/first/a.txt", f"$(B)/{PROJECT}/generated.txt", f"$(B)/{PROJECT}/second/b.txt"])
