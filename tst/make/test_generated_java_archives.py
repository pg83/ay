from tst.make.lib import MakeTest, PROJECT


class GeneratedJavaArchivesTest(MakeTest):
    def check_archive(self, extension):
        filename = "generated" + extension
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PYTHON3(generate.py OUT {filename})\nEND()\n")
        if extension == ".jsrc":
            script = 'import zipfile\nwith zipfile.ZipFile("generated.jsrc", "w") as out:\n    out.writestr("Generated.java", "public class Generated {}\\n")\n'
        else:
            script = 'import io, tarfile\nvalue = b"public class Generated {}\\n"\nwith tarfile.open("generated.gentar", "w") as out:\n    entry = tarfile.TarInfo("Generated.java")\n    entry.size = len(value)\n    out.addfile(entry, io.BytesIO(value))\n'
        self.write("generate.py", script)
        graph = self.graph()
        generated = self.node(graph, filename)
        compiled = self.node(graph, "answer.jar")
        self.assertIn(f"$(B)/{PROJECT}/{filename}", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_generated_source_jars_are_compilation_inputs(self):
        self.check_archive(".jsrc")

    def test_generated_source_tars_are_compilation_inputs(self):
        self.check_archive(".gentar")
