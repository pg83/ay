from tst.make.lib import MakeTest, PROJECT


class CodegenTest(MakeTest):
    def generated(self, source, output, text, macros=""):
        self.write("ya.make", f"LIBRARY()\n{macros}\nSRCS({source})\nEND()\n")
        self.write(source, text)
        graph = self.graph()
        producer = self.node(graph, output)
        self.assertIn(f"$(S)/{PROJECT}/{source}", producer["inputs"])
        return graph

    def test_bison(self):
        self.generated("parser.y", "parser.y.cpp", "%token NUMBER\n%%\nexpression: NUMBER;\n%%\n")

    def test_flex(self):
        self.generated("lexer.l", "lexer.l.cpp", '%option noyywrap\n%%\n[0-9]+ return 1;\n. ;\n%%\n')

    def test_flex_flags(self):
        lexer = '%option noyywrap\n%%\n[0-9]+ return 1;\n. ;\n%%\n'
        for macros, flags in (("SET(LEX_FLAGS -+)", ["-+"]), ("FLEX_FLAGS(-+ --nounistd)", ["-+", "--nounistd"])):
            with self.subTest(macros=macros):
                graph = self.generated("lexer.l", "lexer.l.cpp", lexer, macros)
                args = self.node(graph, "lexer.l.cpp")["cmds"][0]["cmd_args"]
                self.assertEqual(args[1:1 + len(flags)], flags)
                self.assertRegex(args[1 + len(flags)], f"^-o\\$\\(B(UILD_ROOT)?\\)/{PROJECT}/lexer\\.l\\.cpp$")

    def flex_tool_graph(self, before, after=""):
        self.write("ya.make", f"LIBRARY()\n{before}\nSRCS(lexer.lpp answer.cpp)\n{after}\nEND()\n")
        self.write("lexer.lpp", '%option noyywrap\n%%\n[0-9]+ return 1;\n. ;\n%%\n')
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        lexer = self.node(graph, "lexer.lpp.cpp")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in lexer["cmds"] for arg in command["cmd_args"]]
        includes = {
            output: [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in self.node(graph, output)["cmds"] for arg in command["cmd_args"] if "contrib/tools/flex" in arg]
            for output in ("lexer.lpp.cpp.o", "answer.cpp.o")
        }
        return graph, lexer, args, includes

    def test_flex_tool_default(self):
        graph, lexer, args, includes = self.flex_tool_graph("")
        self.assertEqual(args, ["$(B)/contrib/tools/flex-old/flex", f"-o$(B)/{PROJECT}/lexer.lpp.cpp", f"$(S)/{PROJECT}/lexer.lpp"])
        self.assertNotIn("$(B)/contrib/tools/m4/m4", lexer["inputs"])
        self.assertEqual(includes, {output: ["-I$(S)/contrib/tools/flex-old"] for output in includes})

    def test_use_modern_flex(self):
        for before, after in (("USE_MODERN_FLEX()", ""), ("", "USE_MODERN_FLEX()"), ("USE_OLD_FLEX()", "USE_MODERN_FLEX()")):
            with self.subTest(before=before, after=after):
                graph, lexer, args, includes = self.flex_tool_graph(before, after)
                self.assertEqual(args, ["$(B)/contrib/tools/flex/flex", "--m4=$(B)/contrib/tools/m4/m4", f"-o$(B)/{PROJECT}/lexer.lpp.cpp", f"$(S)/{PROJECT}/lexer.lpp"])
                self.assertIn("$(B)/contrib/tools/flex/flex", lexer["inputs"])
                self.assertIn("$(B)/contrib/tools/m4/m4", lexer["inputs"])
                self.assertEqual(includes, {output: ["-I$(S)/contrib/tools/flex"] for output in includes})
                self.assertTrue(any("$(B)/contrib/tools/flex/flex" in node["outputs"] for node in graph["graph"]))

    def test_use_old_flex(self):
        graph, lexer, args, includes = self.flex_tool_graph("USE_MODERN_FLEX()", "USE_OLD_FLEX()")
        self.assertEqual(args, ["$(B)/contrib/tools/flex-old/flex", f"-o$(B)/{PROJECT}/lexer.lpp.cpp", f"$(S)/{PROJECT}/lexer.lpp"])
        self.assertEqual(includes, {output: ["-I$(S)/contrib/tools/flex-old"] for output in includes})

    def test_ragel(self):
        self.generated("machine.rl6", "machine.rl6.cpp", '%%{ machine scanner; main := any*; }%%\n%% write data;\n')

    def test_buildwith_ragel6(self):
        self.write("ya.make", "LIBRARY(answer)\nBUILDWITH_RAGEL6(lexer.rl -e -CF1)\nSRCS(answer.cpp)\nEND()\n")
        self.write("lexer.rl", '%%{ machine scanner; main := any*; }%%\n%% write data;\n')
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        ragel = self.node(graph, "lexer.rl6.cpp")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in ragel["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[-6:], ["-e", "-CF1", "-I$(S)", "-o", f"$(B)/{PROJECT}/lexer.rl6.cpp", f"$(S)/{PROJECT}/lexer.rl"])
        self.assertNotIn("-L", args)
        self.assertEqual(ragel["kv"]["p"], "R6")
        objects = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in self.node(graph, "libanswer.a")["cmds"] for arg in command["cmd_args"] if arg.endswith(".o")]
        self.assertEqual(objects, [f"$(B)/{PROJECT}/answer.cpp.o", f"$(B)/{PROJECT}/lexer.rl6.cpp.o"])

    def test_gperf(self):
        self.generated("keywords.gperf", "keywords.gperf.cpp", "%%\nhello\nworld\n%%\n")

    def test_flatbuffers_import(self):
        self.write("base.fbs", "namespace e2e; table Base { value:int; }\n")
        self.generated("message.fbs", "message.fbs.h", f'include "{PROJECT}/base.fbs";\nnamespace e2e; table Message {{ base:Base; }}\nroot_type Message;\n', "SRCS(base.fbs)")

    def test_configure_file(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_VALUE 42)\nCONFIGURE_FILE(config.h.in config.h)\nSRCS(main.cpp)\nEND()\n")
        self.write("config.h.in", "#define E2E_VALUE @E2E_VALUE@\n")
        self.write("main.cpp", '#include "config.h"\nint answer() { return E2E_VALUE; }\n')
        graph = self.graph()
        self.node(graph, "config.h")
        self.assertIn(f"$(B)/{PROJECT}/config.h", self.node(graph, "main.cpp.o")["inputs"])

    def test_run_python_generates_source(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py IN value.txt OUT answer.cpp)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("answer.cpp").write_text("int answer() { return 42; }\\n")\n')
        self.write("value.txt", "42\n")
        graph = self.graph()
        producer = self.node(graph, "answer.cpp")
        self.assertIn(f"$(S)/{PROJECT}/value.txt", producer["inputs"])
        self.node(graph, "answer.cpp.o")

    def test_cython(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.pyx)\nEND()\n")
        self.write("answer.pyx", "def answer():\n    return 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.pyx" in node["inputs"] and any(path.endswith((".cpp", ".c")) for path in node["outputs"]) for node in graph["graph"]))
