from tst.make.lib import MakeTest, PROJECT


class LateHostFlatbufferToolTest(MakeTest):
    def test_platform_specific_host_peers_keep_their_deferred_flatbuffer_generator(self):
        self.write("ya.make", f"LIBRARY(answer)\nRUN_PROGRAM({PROJECT}/tool STDOUT generated.cpp)\nEND()\n")
        self.write("tool/ya.make", f"IF(ARCH_X86_64)\nPROGRAM(host_tool)\nPEERDIR({PROJECT}/schema)\nELSE()\nPROGRAM(target_tool)\nENDIF()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", '#include <cstdio>\nint main() { std::puts("int answer() { return 42; }"); }\n')
        self.write("schema/ya.make", "FBS_LIBRARY(schema)\nONLY_TAGS(CPP_FBS)\nSRCS(answer.fbs64)\nEND()\n")
        self.write("schema/answer.fbs64", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        graph = self.graph("--target-platform=default-linux-aarch64", "--host-platform=default-linux-x86_64")
        host = self.node(graph, "tool/host_tool")
        flatbuffer = self.node(graph, "schema/answer.fbs64.h")
        nodes = {node["uid"]: node for node in graph["graph"]}
        pending = list(self.node(graph, "generated.cpp")["deps"])
        visited = set()
        while pending:
            uid = pending.pop()
            if uid not in visited:
                visited.add(uid)
                pending.extend(nodes[uid]["deps"])
        self.assertIn(host["uid"], visited)
        self.assertIn(flatbuffer["uid"], visited)
