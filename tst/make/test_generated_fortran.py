import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class GeneratedFortranTest(MakeTest):
    def check_compilation(self, graph, output, producer):
        compiled = self.node(graph, output + ".o")
        self.assertIn(f"$(B)/{PROJECT}/{output}", compiled["inputs"])
        self.assertIn(producer["uid"], compiled["deps"])
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.endswith("/bin/flang-new") for arg in args), args)

    def test_python_generated_fortran_sources_are_compiled(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py OUT answer.f other.f)\nEND()\n")
        self.write("generate.py", "from pathlib import Path\nfor name in ('answer', 'other'):\n    Path(name + '.f').write_text('      INTEGER FUNCTION ' + name + '()\\n      ' + name + ' = 42\\n      END\\n')\n")
        graph = self.graph()
        producer = self.node(graph, "answer.f")
        for output in ("answer.f", "other.f"):
            with self.subTest(output=output):
                self.check_compilation(graph, output, producer)

    def test_program_stdout_generates_a_fortran_source(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/generator STDOUT answer.f)\nEND()\n")
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", '#include <cstdio>\nint main() { std::puts("      INTEGER FUNCTION ANSWER()\\n      ANSWER = 42\\n      END"); }\n')
        graph = self.graph()
        self.check_compilation(graph, "answer.f", self.node(graph, "answer.f"))

    def test_fortran_sources_extracted_from_archives_are_compiled(self):
        self.write("ya.make", "LIBRARY()\nFROM_ARCHIVE(sources.tar OUT answer.f)\nEND()\n")
        value = b"      INTEGER FUNCTION ANSWER()\n      ANSWER = 42\n      END\n"
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            entry = tarfile.TarInfo("answer.f")
            entry.size = len(value)
            archive.addfile(entry, io.BytesIO(value))
        graph = self.graph()
        self.check_compilation(graph, "answer.f", self.node(graph, "answer.f"))
