from tst.make.lib import MakeTest, PROJECT


class LinkExclusionPeersTest(MakeTest):
    def exclusions(self, policy):
        self.write("ya.make", f"PROGRAM(answer)\nPEERDIR({PROJECT}/policy)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("policy/ya.make", policy)
        node = self.node(self.graph(), "answer")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        return [args[i + 1] for i, arg in enumerate(args) if arg == "--exclude-libs"]

    def test_resource_peers_do_not_export_library_link_settings(self):
        exclusions = self.exclusions("RESOURCES_LIBRARY()\nLINK_EXCLUDE_LIBRARIES(libalpha.a libbeta.a)\nEND()\n")
        self.assertNotIn("libalpha.a", exclusions)
        self.assertNotIn("libbeta.a", exclusions)

    def test_native_library_peers_export_link_library_exclusions(self):
        self.write("policy/policy.cpp", "int policy() { return 42; }\n")
        exclusions = self.exclusions("LIBRARY(policy)\nSRCS(policy.cpp)\nLINK_EXCLUDE_LIBRARIES(libalpha.a libbeta.a)\nEND()\n")
        self.assertIn("libalpha.a", exclusions)
        self.assertIn("libbeta.a", exclusions)
