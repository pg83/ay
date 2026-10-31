from tst.make.lib import MakeTest, PROJECT


class RootLinkerPluginsTest(MakeTest):
    def test_a_root_static_library_keeps_its_linker_plugin_producer(self):
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\nLD_PLUGIN(plugin.py)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        self.write("plugin.py", "def on_link(args): return args\n")
        copied = self.node(self.graph(), "plugin.py.pyplugin")
        self.assertIn(f"$(S)/{PROJECT}/plugin.py", copied["inputs"])
