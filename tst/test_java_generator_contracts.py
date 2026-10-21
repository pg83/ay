import unittest

import lib
import test_java
from test_java_features import tree


class JavaGeneratorContractsTest(unittest.TestCase):
    def test_generator_rooted_inputs_output_directory_and_working_directory(self):
        files = test_java.JavaProgramTest().tree()
        files["gen/ya.make"] = (
            "JAVA_LIBRARY_NON_SPLIT()\n"
            "RUN_JAVA_PROGRAM(Main ${ARCADIA_ROOT}/gen/in.txt ${ARCADIA_BUILD_ROOT}/app/app.jar "
            "IN ${ARCADIA_ROOT}/gen/in.txt ${ARCADIA_BUILD_ROOT}/app/app.jar "
            "OUT_DIR ${ARCADIA_BUILD_ROOT}/generated CWD ${ARCADIA_ROOT}/gen "
            "CLASSPATH app ADD_SRCS_TO_CLASSPATH --extra)\nEND()\n"
        )
        graph = lib.make(files, "gen")
        generated = next(n for n in graph["graph"] if n["kv"]["p"] == "RJ")
        self.assertTrue(generated["outputs"][0].startswith("$(B)/generated."))
        run = generated["cmds"][1]
        self.assertEqual(run["cwd"], "$(S)/gen")
        self.assertEqual(run["cmd_args"][-4:], ["Main", "$(S)/gen/in.txt", "$(B)/app/app.jar", "--extra"])
        self.assertIn("$(S)/gen/in.txt", generated["inputs"])
        self.assertIn("$(B)/app/app.jar", generated["inputs"])

    def test_generator_without_output_directory_does_not_emit_an_archive(self):
        files = tree("RUN_JAVA_PROGRAM(Main)")
        graph = lib.make(files, "app")
        lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertFalse(any(n["kv"]["p"] == "RJ" for n in graph["graph"]))

    def test_native_generator_rejects_java_library_as_an_executable(self):
        files = tree("")
        files["native/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(app OUT generated.cpp)\nEND()\n"
        result = lib.make_process(files, "native")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("tool app is not a Java program", result.stderr)
        self.assertNotIn("panic:", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
