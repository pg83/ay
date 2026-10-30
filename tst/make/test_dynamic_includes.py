from tst.make.lib import MakeTest, PROJECT


class DynamicIncludesTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_include_once_applies_to_variable_selected_files(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_INCLUDE flags.inc)\nINCLUDE(${E2E_INCLUDE})\nINCLUDE(${E2E_INCLUDE})\nSRCS(answer.cpp)\nEND()\n")
        self.write("flags.inc", "INCLUDE_ONCE()\nCFLAGS(-DE2E_ONCE=42)\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args.count("-DE2E_ONCE=42"), 1)

    def test_dynamic_nested_includes_are_relative_to_the_including_file(self):
        self.write("ya.make", f"LIBRARY()\nSET(E2E_FIRST ${{ARCADIA_ROOT}}/{PROJECT}/includes/first.inc)\nSET(E2E_NEXT second.inc)\nINCLUDE(${{E2E_FIRST}})\nSRCS(answer.cpp)\nEND()\n")
        self.write("includes/first.inc", "CFLAGS(-DE2E_FIRST=1)\nINCLUDE(${E2E_NEXT})\n")
        self.write("includes/second.inc", "CFLAGS(-DE2E_SECOND=2)\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        for flag in ("-DE2E_FIRST=1", "-DE2E_SECOND=2"):
            self.assertIn(flag, args)

    def test_dynamic_includes_require_existing_source_files(self):
        self.write("flags.inc", "CFLAGS(-DE2E_INCLUDE=42)\n")
        for path in (f"${{ARCADIA_BUILD_ROOT}}/{PROJECT}/flags.inc", "/ay_e2e_missing.inc", "missing.inc"):
            with self.subTest(path=path):
                self.write("ya.make", f"LIBRARY()\nSET(E2E_INCLUDE {path})\nINCLUDE(${{E2E_INCLUDE}})\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_dynamic_include_cycles_are_configuration_errors(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_FIRST first.inc)\nSET(E2E_SECOND second.inc)\nINCLUDE(${E2E_FIRST})\nSRCS(answer.cpp)\nEND()\n")
        self.write("first.inc", "INCLUDE_ONCE(no)\nINCLUDE(${E2E_SECOND})\n")
        self.write("second.inc", "INCLUDE_ONCE(no)\nINCLUDE(${E2E_FIRST})\n")
        self.assertNotEqual(self.make().returncode, 0)
