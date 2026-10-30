from tst.make.lib import MakeTest, PROJECT


class JavaFileRegistrationTest(MakeTest):
    def test_non_java_files_are_not_compilation_inputs(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nFILES(value.txt)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("value.txt", "42\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{PROJECT}/Answer.java", node["inputs"])
        self.assertNotIn(f"$(B)/{PROJECT}/value.txt", node["inputs"])

    def test_repeated_source_files_are_rejected(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nFILES(Answer.java Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_external_java_libraries_use_generated_jars_only(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/external)\nEND()\n")
        self.write("external/ya.make", "EXTERNAL_JAVA_LIBRARY()\nJDK_VERSION(17)\nRUN_PYTHON3(generate.py OUT generated.jar ignored.txt)\nEND()\n")
        self.write("external/generate.py", 'import zipfile\nfrom pathlib import Path\nwith zipfile.ZipFile("generated.jar", "w") as jar:\n    jar.writestr("META-INF/MANIFEST.MF", "Manifest-Version: 1.0\\n")\nPath("ignored.txt").write_text("42\\n")\n')
        graph = self.graph()
        generated = self.node(graph, "external/generated.jar")
        consumers = [node for node in graph["graph"] if generated["uid"] in node["deps"]]
        self.assertTrue(consumers)
        self.assertTrue(any(f"$(B)/{PROJECT}/external/generated.jar" in node["inputs"] for node in consumers))
        self.assertFalse(any(f"$(B)/{PROJECT}/external/ignored.txt" in node["inputs"] for node in consumers))
