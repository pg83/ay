from tst.make.lib import MakeTest, PROJECT


class DynamicLibraryOptionsTest(MakeTest):
    def library(self, settings="", peers=None):
        if peers is None:
            peers = [f"{PROJECT}/static"]
        self.write("ya.make", f"DYNAMIC_LIBRARY(answer)\nDYNAMIC_LIBRARY_FROM({' '.join(peers)})\nEXPORTS_SCRIPT(symbols.exports)\n{settings}\nEND()\n")
        self.write("symbols.exports", "C answer\n")
        self.write("static/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("static/answer.cpp", 'extern "C" int answer() { return 42; }\n')

    def test_dynamic_libraries_can_omit_archive_declarations_and_the_name(self):
        for declaration, output in (("DYNAMIC_LIBRARY()", f"lib{PROJECT}.so"),
                                    ("DYNAMIC_LIBRARY(answer)", "libanswer.so")):
            with self.subTest(declaration=declaration):
                self.write("ya.make", declaration + "\nEND()\n")
                self.node(self.graph(), output)

    def test_dynamic_libraries_can_link_archives_without_an_exports_script(self):
        self.library()
        self.write("ya.make", f"DYNAMIC_LIBRARY(answer)\nDYNAMIC_LIBRARY_FROM({PROJECT}/static)\nEND()\n")
        self.node(self.graph(), "libanswer.so")

    def test_duplicate_archive_and_rpath_helper_peers_do_not_duplicate_inputs(self):
        self.library(peers=[f"{PROJECT}/static", "build/platform/local_so", f"{PROJECT}/static"])
        self.write("static/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nSET_APPEND(RPATH_GLOBAL -Wl,-rpath,e2e)\nEND()\n")
        linked = self.node(self.graph(), "libanswer.so")
        self.assertEqual(linked["inputs"].count(f"$(B)/{PROJECT}/static/lib{PROJECT}-static.a"), 1)

    def test_shared_library_copy_mode_keeps_the_filesystem_helper(self):
        self.library("ENABLE(SO_OUTPUTS)")
        linked = self.node(self.graph(), "libanswer.so")
        args = [arg for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.endswith("/build/scripts/fs_tools.py") for arg in args), args)
        self.assertIn("$(S)/build/scripts/fs_tools.py", linked["inputs"])

    def test_linker_plugins_reach_dynamic_library_link_commands(self):
        self.library()
        self.write("static/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nLD_PLUGIN(plugin.py)\nEND()\n")
        self.write("static/plugin.py", "def onlink_cmd(cmd): return cmd\n")
        linked = self.node(self.graph(), "libanswer.so")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(B)/{PROJECT}/static/plugin.py.pyplugin", args)

    def test_deprecated_dll_exports_argument_is_rejected(self):
        self.write("ya.make", "DLL(answer EXPORTS symbols.exports)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("symbols.exports", "C answer\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_windows_python_modules_use_the_pyd_extension(self):
        self.write("ya.make", "PY3MODULE(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.node(self.graph("--target-platform=default-win-x86_64"), "answer.pyd")
