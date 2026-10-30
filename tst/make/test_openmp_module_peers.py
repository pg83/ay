from tst.make.lib import MakeTest


class OpenmpModulePeersTest(MakeTest):
    def test_enabling_openmp_adds_its_runtime_link_dependency(self):
        self.write("ya.make", "PROGRAM(answer)\nSET(USE_OPENMP yes)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        node = self.node(self.graph(), "answer")
        self.assertTrue(any(path.startswith("$(B)/contrib/libs/cxxsupp/openmp/") and path.endswith(".a") for path in node["inputs"]), node["inputs"])
