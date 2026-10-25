from tst.make.lib import MakeTest, PROJECT


class JavaMetadataTest(MakeTest):
    def test_sources_jar_default_and_overrides(self):
        self.write("Answer.java", "public class Answer {}\n")
        cases = (
            (("--build=debug",), "", True),
            (("--build=release",), "", False),
            (("--build=debug", "-DNO_DEBUGINFO=yes"), "", False),
            (("--build=debug",), "DISABLE(SOURCES_JAR)", False),
            (("--build=release",), "ENABLE(SOURCES_JAR)", True),
            (("--build=debug",), "SET(SOURCES_JAR no)", False),
        )
        for flags, macro, expected in cases:
            with self.subTest(flags=flags, macro=macro):
                self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\n{macro}\nEND()\n")
                node = self.node(self.graph(*flags), "answer.jar")
                self.assertEqual(f"$(B)/{PROJECT}/answer-sources.jar" in node["outputs"], expected)
                prepare = next(cmd["cmd_args"] for cmd in node["cmds"] if any(arg.endswith("/prepare_jar_build.py") for arg in cmd["cmd_args"]))
                self.assertEqual("--with-sources-jar" in prepare, expected)

    def test_sbom_components_and_toolchain_inputs(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nENABLE(EMBED_SBOM)\nPEERDIR({PROJECT}/api)\nEND()\n")
        self.write("api/ya.make", f"JAVA_LIBRARY(api)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/contrib)\nEND()\n")
        self.write("contrib/ya.make", "JAVA_CONTRIB(dependency)\nVERSION(1 2 3)\nLICENSE(MIT)\nLOCAL_JAR(dependency.jar)\nLOCAL_SOURCES_JAR(sources.jar)\nEND()\n")
        self.write("contrib/dependency.jar", "")
        self.write("contrib/sources.jar", "")
        graph = self.graph("--build=release")
        component = self.node(graph, "contrib/dependency.component.sbom")
        self.assertIn(f"$(B)/{PROJECT}/contrib/dependency.jar", component["outputs"])
        generate = next(cmd["cmd_args"] for cmd in component["cmds"] if any(arg.endswith("/gen_sbom.py") for arg in cmd["cmd_args"]))
        self.assertEqual(generate[generate.index("--ver") + 1], "1.2.3")
        self.assertEqual(generate[generate.index("--lang") + 1], "JAVA")
        for output in ("answer.jar", "answer.run.cp.jar", "api/api.jar", "api/api-interface.jar"):
            node = self.node(graph, output)
            for tool in ("java/jdk/jdk17", "python/ymake_python3"):
                path = f"$(B)/build/platform/{tool}/toolchain.component.sbom"
                self.assertIn(path, node["inputs"])
                self.assertTrue(any(path in dep["outputs"] and dep["uid"] in node["deps"] for dep in graph["graph"]))
        jar = self.node(graph, "answer.jar")
        self.assertTrue(any(any(arg.endswith("/link_java_sbom.py") for arg in cmd["cmd_args"]) for cmd in jar["cmds"]))

    def test_sbom_can_be_disabled(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nDISABLE(EMBED_SBOM)\nPEERDIR({PROJECT}/contrib)\nEND()\n")
        self.write("contrib/ya.make", "JAVA_CONTRIB(dependency)\nDISABLE(_NEED_SBOM_INFO)\nVERSION(1)\nLICENSE(MIT)\nLOCAL_JAR(dependency.jar)\nLOCAL_SOURCES_JAR(sources.jar)\nEND()\n")
        self.write("contrib/dependency.jar", "")
        self.write("contrib/sources.jar", "")
        graph = self.graph("--build=release", "-DSBOM_GENERATION_ALLOWED=no")
        jar = self.node(graph, "answer.jar")
        self.assertFalse(any(any(arg.endswith("/link_java_sbom.py") for arg in cmd["cmd_args"]) for cmd in jar["cmds"]))
        contrib = self.node(graph, "contrib/dependency.jar")
        self.assertFalse(any(path.endswith(".component.sbom") for path in contrib["outputs"]))
        self.assertFalse(any(path.endswith(".component.sbom") for path in jar["inputs"]))

    def test_kotlin_sbom_toolchain(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        jar = self.node(self.graph("--build=release"), "answer.jar")
        command = next(cmd["cmd_args"] for cmd in jar["cmds"] if any(arg.endswith("/link_java_sbom.py") for arg in cmd["cmd_args"]))
        toolchains = [command[i + 1].replace("$(BUILD_ROOT)", "$(B)") for i, arg in enumerate(command) if arg == "--toolchain"]
        self.assertEqual(toolchains, [
            "$(B)/build/platform/java/jdk/jdk17/toolchain.component.sbom",
            "$(B)/build/platform/java/kotlin/toolchain.component.sbom",
        ])
        for path in toolchains:
            self.assertIn(path, jar["inputs"])

    def test_sbom_module_flags(self):
        cases = (
            ((), "DISABLE(EMBED_SBOM)", True, True),
            (("-DEMBED_SBOM=no",), "", True, True),
            ((), "DISABLE(_NEED_SBOM_INFO)", True, False),
            (("-DAUTOCHECK=yes",), "", False, False),
            (("-DAUTOCHECK=yes",), "ENABLE(EMBED_SBOM)", True, False),
        )
        for flags, macro, expected, toolchain in cases:
            with self.subTest(flags=flags, macro=macro):
                self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\n{macro}\nEND()\n")
                jar = self.node(self.graph("--build=release", *flags), "answer.jar")
                commands = [cmd["cmd_args"] for cmd in jar["cmds"] if any(arg.endswith("/link_java_sbom.py") for arg in cmd["cmd_args"])]
                self.assertEqual(bool(commands), expected)
                if commands:
                    self.assertEqual("--toolchain" in commands[0], toolchain)
