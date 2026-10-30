from tst.make.lib import MakeTest


class RootDirectoryDeclarationsTest(MakeTest):
    def test_a_source_root_directory_declaration_keeps_the_root_file(self):
        self.write_tree("__ay_root_e2e__.txt", "42\n")
        self.write("ya.make", "LIBRARY()\nDECLARE_IN_DIRS(ROOT __ay_root_e2e__.txt SRCDIR ${ARCADIA_ROOT} DIRS .)\nRUN_PYTHON3(generate.py IN ${ROOT_FILES} STDOUT value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        node = self.node(self.graph(), "value.txt")
        self.assertIn("$(S)/__ay_root_e2e__.txt", node["inputs"])
