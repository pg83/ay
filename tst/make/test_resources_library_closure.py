from tst.make.lib import MakeTest, PROJECT


class ResourcesLibraryClosureTest(MakeTest):
    def consumer(self):
        self.write("ya.make", f"UNITTEST(answer)\nSRCS(answer_ut.cpp)\nPEERDIR({PROJECT}/resources)\nEND()\n")
        self.write("answer_ut.cpp", "#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT(true); } }\n")

    def marker_arguments(self, graph):
        suite = self.node(graph, "test-results/unittest/meta.json")
        return [arg for command in suite["cmds"] for arg in command["cmd_args"]
                if arg.startswith("E2E_MARK_RESOURCE_GLOBAL::")]

    def test_resource_libraries_export_own_resources_and_native_peer_globals(self):
        self.consumer()
        self.write("resources/ya.make", f"RESOURCES_LIBRARY()\nSET_APPEND(PEERDIR_TAGS __EMPTY__)\nDECLARE_EXTERNAL_RESOURCE(E2E_MARK sbr:123456789)\nRESOURCE(value.txt /value)\nADDINCL(GLOBAL {PROJECT}/include)\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("resources/value.txt", "42\n")
        self.write("include/own.h", "#pragma once\n")
        self.write("native/ya.make", f"LIBRARY(native)\nSRCS(GLOBAL value.cpp)\nADDINCL(GLOBAL {PROJECT}/native/include)\nEND()\n")
        self.write("native/value.cpp", "int answer() { return 42; }\n")
        self.write("native/include/peer.h", "#pragma once\n")
        self.write("answer_ut.cpp", '#include "own.h"\n#include "peer.h"\n' + (self.project / "answer_ut.cpp").read_text())
        graph = self.graph("-t")
        self.assertEqual(len(self.marker_arguments(graph)), 1)
        compiled = self.node(graph, "answer_ut.cpp.o")
        for header in ("include/own.h", "native/include/peer.h"):
            self.assertIn(f"$(S)/{PROJECT}/{header}", compiled["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/native/libnative.global.a", self.node(graph, "answer")["inputs"])

    def test_repeated_resource_declarations_export_one_global(self):
        self.consumer()
        declaration = "DECLARE_EXTERNAL_RESOURCE(E2E_MARK sbr:123456789)\n"
        self.write("resources/ya.make", "RESOURCES_LIBRARY()\n" + declaration * 2 + "END()\n")
        self.assertEqual(len(self.marker_arguments(self.graph("-t"))), 1)
