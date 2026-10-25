import unittest

from tst import lib


class JavaExternalGeneratorsTest(unittest.TestCase):
    def graph(self, body, **extra):
        return lib.make(lib.java_tree(**{
            "generated/ya.make": "EXTERNAL_JAVA_LIBRARY(assembled)\n" + body + "\nEND()\n",
            "generated/writer.py": "import sys\n",
            **extra,
        }), "generated")

    def test_resource_to_python_to_external_jar_preserves_real_inputs(self):
        graph = self.graph("""
SET(DATA seed.dict seed.weights)
SET(OUTPUT staging.jar)
FROM_SANDBOX(37 OUT $DATA)
RUN_PYTHON3(writer.py $OUTPUT $DATA IN $DATA OUT $OUTPUT)
SET(DATA changed.dict)
SET(OUTPUT changed.jar)
""")
        sandbox = lib.node_by_output(graph, "$(B)/generated/seed.dict")
        python = lib.node_by_output(graph, "$(B)/generated/staging.jar")
        jar = lib.node_by_output(graph, "$(B)/generated/assembled.jar")
        self.assertEqual(python["kv"]["p"], "PY")
        self.assertEqual(python["cmds"][0]["cmd_args"], [
            "$(B)/resources/YMAKE_PYTHON3/bin/python3", "$(S)/generated/writer.py",
            "$(B)/generated/staging.jar", "$(B)/generated/seed.dict", "$(B)/generated/seed.weights",
        ])
        self.assertIn(sandbox["uid"], python["deps"])
        self.assertIn(python["uid"], jar["deps"])
        self.assertIn("$(S)/generated/writer.py", python["inputs"])
        for data in ("seed.dict", "seed.weights"):
            self.assertIn("$(B)/generated/" + data, python["inputs"])
            self.assertNotIn("$(B)/generated/" + data, jar["inputs"])
        self.assertIn("$(B)/generated/staging.jar", jar["inputs"])
        self.assertIn("$(B)/generated/staging.jar", jar["cmds"][0]["cmd_args"])
        self.assertIn(jar["uid"], graph["result"])

    def test_declared_producer_order_resolves_chains_and_unparsed_inputs(self):
        graph = self.graph("""
IF (MODULE_TAG == JAVA)
    RUN_PYTHON3(writer.py final.jar intermediate.bin IN intermediate.bin IN_NOPARSE local.h OUT final.jar ENV MODE=java CWD ${BINDIR})
ELSE()
    RUN_PYTHON3(writer.py OUT wrong.jar)
ENDIF()
RUN_PYTHON3(writer.py intermediate.bin seed.dict IN seed.dict OUT intermediate.bin)
FROM_SANDBOX(41 OUT_NOAUTO seed.dict)
""", **{"generated/local.h": '#include "missing.h"\n'})
        sandbox = lib.node_by_output(graph, "$(B)/generated/seed.dict")
        intermediate = lib.node_by_output(graph, "$(B)/generated/intermediate.bin")
        final = lib.node_by_output(graph, "$(B)/generated/final.jar")
        jar = lib.node_by_output(graph, "$(B)/generated/assembled.jar")
        self.assertIn(sandbox["uid"], intermediate["deps"])
        self.assertIn(intermediate["uid"], final["deps"])
        self.assertIn(final["uid"], jar["deps"])
        self.assertIn("$(S)/generated/local.h", final["inputs"])
        self.assertNotIn("$(S)/generated/missing.h", final["inputs"])
        self.assertEqual(final["cmds"][0]["cwd"], "$(B)/generated")
        self.assertEqual(final["cmds"][0]["env"]["MODE"], "java")
        self.assertNotIn("$(B)/generated/intermediate.bin", jar["inputs"])
        self.assertFalse(any("$(B)/generated/wrong.jar" in n["outputs"] for n in graph["graph"]))

    def test_direct_extension_inputs_precede_noauto_and_keep_output_order(self):
        graph = self.graph("""
FROM_SANDBOX(43 OUT bundled.jar OUT_NOAUTO hidden.mf raw.dat)
RUN_PYTHON3(writer.py first.jsrc side.java ignored.txt OUT first.jsrc OUT_NOAUTO side.java ignored.txt STDOUT_NOAUTO stdout.jar)
""")
        jar = lib.node_by_output(graph, "$(B)/generated/assembled.jar")
        args = jar["cmds"][0]["cmd_args"]
        start = args.index("--srcs-jar-output") + 2
        self.assertEqual(args[start:args.index("DELIM")], [
            "$(B)/generated/bundled.jar", "$(B)/generated/hidden.mf",
            "$(B)/generated/first.jsrc", "$(B)/generated/side.java", "$(B)/generated/stdout.jar",
        ])
        self.assertNotIn("$(B)/generated/raw.dat", jar["inputs"])
        self.assertNotIn("$(B)/generated/ignored.txt", jar["inputs"])
        self.assertEqual(len(jar["deps"]), len(set(jar["deps"])))

    def test_supported_copied_files_are_external_auto_inputs(self):
        graph = self.graph("FILES(local.jar local.mf ignored.txt)", **{
            "generated/local.jar": "jar", "generated/local.mf": "manifest",
            "generated/ignored.txt": "not a module input",
        })
        jar = lib.node_by_output(graph, "$(B)/generated/assembled.jar")
        args = jar["cmds"][0]["cmd_args"]
        for file in ("local.jar", "local.mf"):
            copied = lib.node_by_output(graph, "$(B)/generated/" + file)
            self.assertIn(copied["uid"], jar["deps"])
            self.assertIn("$(B)/generated/" + file, args)
        self.assertNotIn("$(B)/generated/ignored.txt", jar["inputs"])

    def test_ordinary_jar_consumes_generated_java_types_without_raw_outputs(self):
        graph = lib.make(lib.java_tree(**{
            "library/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nRUN_PYTHON3(writer.py generated.jsrc generated.kt raw.dat OUT generated.jsrc generated.kt raw.dat)\nEND()\n",
            "library/writer.py": "import sys\n",
        }), "library")
        generator = lib.node_by_output(graph, "$(B)/library/generated.jsrc")
        jar = lib.node_by_output(graph, "$(B)/library/library.jar")
        prepare = next(command["cmd_args"] for command in jar["cmds"]
                       if any(arg.endswith("/prepare_jar_build.py") for arg in command["cmd_args"]))
        self.assertIn(generator["uid"], jar["deps"])
        for name in ("generated.jsrc", "generated.kt"):
            self.assertIn("$(B)/library/" + name, prepare)
            self.assertIn("$(B)/library/" + name, jar["inputs"])
        self.assertNotIn("$(B)/library/raw.dat", jar["inputs"])

    def test_explicit_java_srcs_consumes_noauto_python_resource(self):
        graph = lib.make(lib.java_tree(**{
            "resources/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nRUN_PYTHON3(writer.py contents.dat OUT_NOAUTO contents.dat)\nJAVA_SRCS(RESOURCES SRCDIR ${BINDIR} FILES contents.dat)\nEND()\n",
            "resources/writer.py": "import sys\n",
        }), "resources")
        generator = lib.node_by_output(graph, "$(B)/resources/contents.dat")
        jar = lib.node_by_output(graph, "$(B)/resources/resources.jar")
        self.assertIn(generator["uid"], jar["deps"])
        self.assertIn("$(B)/resources/contents.dat", jar["inputs"])
        self.assertTrue(any("$(B)/resources/contents.dat" in command["cmd_args"]
                            for command in jar["cmds"]))

    def test_deferred_suites_preserve_declarations_after_producer_frame_reuse(self):
        files = lib.java_tree(**{
            "generated/ya.make": "EXTERNAL_JAVA_LIBRARY(assembled)\nFROM_SANDBOX(53 OUT seed.dict)\nRUN_PYTHON3(writer.py staging.jar seed.dict IN seed.dict OUT staging.jar)\nEND()\n",
            "generated/writer.py": "import sys\n",
            "later/ya.make": "EXTERNAL_JAVA_LIBRARY(second)\nFROM_SANDBOX(FILE 59 OUT imported.jar)\nEND()\n",
        })
        graph = lib.make(files, "generated", "later", "--target-platform-tests", opensource=False)
        sandbox = lib.node_by_output(graph, "$(B)/generated/seed.dict")
        python = lib.node_by_output(graph, "$(B)/generated/staging.jar")
        jar = lib.node_by_output(graph, "$(B)/generated/assembled.jar")
        self.assertIn(sandbox["uid"], python["deps"])
        self.assertIn(python["uid"], jar["deps"])
        validation = next(node for node in graph["graph"]
                          if node["kv"].get("path") == "generated/validate_resource")
        self.assertTrue(any("53" in command["cmd_args"] for command in validation["cmds"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
