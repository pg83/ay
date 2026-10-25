from tst.make.lib import MakeTest


class PeerPolicyTest(MakeTest):
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
