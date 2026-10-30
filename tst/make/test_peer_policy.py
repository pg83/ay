from tst.make.lib import MakeTest, PROJECT


class PeerPolicyTest(MakeTest):
    def policy_fixture(self, rule, peer="peer"):
        self.write("rules.txt", rule + "\n")
        self.write_tree("build/internal/conf/rules.conf", f"PEERDIRS_RULES_PATH={PROJECT}/rules.txt\n")
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/{peer})\nEND()\n")
        self.write("answer.cpp", "int main() { return 0; }\n")
        self.write(peer + "/ya.make", "LIBRARY(value)\nSRCS(value.cpp)\nEND()\n")
        self.write(peer + "/value.cpp", "int value() { return 17; }\n")

    def test_suffix_rules_match_complete_directory_suffixes(self):
        for peer, denied in (("peer", True), ("peer/child", False)):
            with self.subTest(peer=peer):
                self.policy_fixture("DENY .* -> .*peer$", peer)
                if denied:
                    result = self.make()
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("peerdir policy", result.stderr)
                else:
                    self.node(self.graph(), peer + "/libvalue.a")

    def test_malformed_policy_rules_are_configuration_errors(self):
        for rule in ("UNKNOWN .* -> .*", "ALLOW .* => .*", "ALLOW .*"):
            with self.subTest(rule=rule):
                self.policy_fixture(rule)
                self.assertNotEqual(self.make().returncode, 0)

    def test_restricted_peer_is_rejected_and_skipped(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nPEERDIR(contrib/restricted/boost/hana)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("peerdir policy", result.stderr)
        self.assertIn("contrib/restricted/boost/hana", result.stderr)
        graph = self.graph("-k")
        self.node(graph, "answer.cpp.o")
        self.assertFalse(any("/contrib/restricted/boost/" in p for n in graph["graph"] for p in n["outputs"]))

    def test_allow_rule_precedes_restricted_default(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nPEERDIR(library/cpp/testing/gtest)\nEND()\n")
        self.write("answer.cpp", '#include <library/cpp/testing/gtest/gtest.h>\nint answer() { return 42; }\n')
        graph = self.graph()
        inputs = self.node(graph, "answer.cpp.o")["inputs"]
        self.assertIn("$(S)/contrib/restricted/googletest/googletest/include/gtest/gtest.h", inputs)
        self.assertIn("$(S)/contrib/restricted/googletest/googlemock/include/gmock/gmock.h", inputs)
