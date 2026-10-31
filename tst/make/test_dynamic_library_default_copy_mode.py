from tst.make.lib import MakeTest


class DynamicLibraryDefaultCopyModeTest(MakeTest):
    def test_linux_dynamic_libraries_work_without_a_shared_output_copy_default(self):
        config = "build/ymake.core.conf"
        original = (self.root / config).read_text()
        default = "\nSO_OUTPUTS=no\n"
        self.assertEqual(original.count(default), 1)
        self.write_tree(config, original.replace(default, "\n"))
        self.write("ya.make", f"DYNAMIC_LIBRARY(answer)\nDYNAMIC_LIBRARY_FROM({self.project_name}/static)\nEXPORTS_SCRIPT(symbols.exports)\nEND()\n")
        self.write("symbols.exports", "C answer\n")
        self.write("static/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("static/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        linked = self.node(self.graph(), "libanswer.so")
        self.assertNotIn("$(S)/build/scripts/fs_tools.py", linked["inputs"])
