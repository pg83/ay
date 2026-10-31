import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class PremoduleDeclarationBatchesTest(MakeTest):
    def test_archive_source_batches_keep_both_output_dependencies(self):
        self.write("ya.make", "LIBRARY()\nUSRV_BUILD(first.cpp second.cpp FROM sources.tar)\nEND()\n")
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            for name in ("first", "second"):
                data = f"int {name}() {{ return 42; }}\n".encode()
                entry = tarfile.TarInfo(name + ".cpp")
                entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))
        graph = self.graph()
        for name in ("first", "second"):
            generated = self.node(graph, name + ".cpp")
            self.assertIn(generated["uid"], self.node(graph, name + ".cpp.o")["deps"])

    def test_python_source_declarations_require_a_module(self):
        self.write("ya.make", "PY_SRCS(main.py)\nPY3_PROGRAM(answer)\nEND()\n")
        self.write("main.py", "def main(): print(42)\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_program_generators_require_a_module(self):
        self.write("ya.make", f"RUN_PROGRAM({PROJECT}/generator OUT generated.cpp)\nLIBRARY()\nEND()\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('int answer() { return 42; }')\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_python_generators_require_a_module(self):
        self.write("ya.make", "RUN_PYTHON3(generate.py STDOUT generated.cpp)\nLIBRARY()\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_bundle_declarations_require_a_module(self):
        self.write("ya.make", f"BUNDLE({PROJECT}/tool NAME bundled)\nLIBRARY()\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_configured_header_declarations_require_a_module(self):
        self.write("ya.make", f"CONFIGURE_FILE(input.h.in output.h)\nLIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nSRCS(answer.cpp)\nEND()\n")
        self.write("input.h.in", "#pragma once\n")
        self.write("answer.cpp", '#include "output.h"\nint answer() { return 42; }\n')
        self.assertNotEqual(self.make().returncode, 0)
