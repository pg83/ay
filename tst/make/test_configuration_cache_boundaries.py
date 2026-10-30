from tst.make.lib import MakeTest


class ConfigurationCacheBoundariesTest(MakeTest):
    def test_pre_module_peer_tag_settings_remain_local_to_each_module(self):
        self.write("ya.make", "RECURSE(first second)\n")
        for directory, tags in (("first", "CPP_PROTO"), ("second", "CPP_PROTO_LEGACY_FOR_DIRECT_HEADERS")):
            self.write(f"{directory}/ya.make", f"SET(_CPP_PROTO_COMPATIBLE_TAGS {tags})\nLIBRARY()\nCFLAGS(-DE2E_PEER_TAGS=${{_CPP_PROTO_COMPATIBLE_TAGS}})\nSRCS(answer.cpp)\nEND()\n")
            self.write(f"{directory}/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        for directory, tags in (("first", "CPP_PROTO"), ("second", "CPP_PROTO_LEGACY_FOR_DIRECT_HEADERS")):
            args = [arg for command in self.node(graph, f"{directory}/answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
            self.assertIn(f"-DE2E_PEER_TAGS={tags}", args)

    def test_nested_configuration_macro_declarations_are_rejected(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_OUTER() {
    CFLAGS(-DE2E_FIRST=1)
    macro E2E_INNER() {
        CFLAGS(-DE2E_INNER=1)
    }
    CFLAGS(-DE2E_LAST=1)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_OUTER()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
