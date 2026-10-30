from tst.make.lib import MakeTest, PROJECT


class ResourcePeerPolicyTest(MakeTest):
    def fixture(self, value):
        self.write("ya.make", f"UNITTEST()\nSRCS(answer_ut.cpp)\nPEERDIR({PROJECT}/middle)\nEND()\n")
        self.write("answer_ut.cpp", "#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT(true); } }\n")
        self.write("middle/ya.make", f"LIBRARY()\nSET(PASS_PEERS {value})\nSRCS(value.cpp)\nPEERDIR({PROJECT}/resources)\nEND()\n")
        self.write("middle/value.cpp", "int answer() { return 42; }\n")
        self.write("resources/ya.make", "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(E2E_MARK sbr:123456789)\nEND()\n")

    def has_marker(self):
        suite = self.node(self.graph("-t"), "test-results/unittest/meta.json")
        return any(arg.startswith("E2E_MARK_RESOURCE_GLOBAL::")
                   for command in suite["cmds"] for arg in command["cmd_args"])

    def test_enabled_peer_passing_exports_transitive_resources(self):
        self.fixture("yes")
        self.assertTrue(self.has_marker())

    def test_disabled_peer_passing_hides_transitive_resources(self):
        self.fixture("no")
        self.assertFalse(self.has_marker())
