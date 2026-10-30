from tst.make.lib import MakeTest


class MissingKnownConfigurationMacrosTest(MakeTest):
    def test_a_removed_dynamic_library_macro_is_rejected_when_used(self):
        core = self.root / "build/ymake.core.conf"
        definition = "macro WITH_DYNAMIC_LIBS() {\n    ENABLE(SO_OUTPUTS)\n}"
        text = core.read_text()
        self.assertIn(definition, text)
        self.write_tree("build/ymake.core.conf", text.replace(definition, "", 1))
        self.write("ya.make", "LIBRARY()\nWITH_DYNAMIC_LIBS()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
