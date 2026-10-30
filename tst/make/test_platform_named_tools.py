from tst.make.lib import MakeTest, PROJECT


class PlatformNamedToolsTest(MakeTest):
    def test_repeated_generators_use_the_host_variant_of_a_platform_named_tool(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/tool STDOUT first.txt)\nRUN_PROGRAM({PROJECT}/tool STDOUT second.txt)\nRESOURCE(first.txt /first second.txt /second)\nEND()\n")
        self.write("tool/ya.make", "IF(ARCH_X86_64)\nPROGRAM(host_tool)\nELSE()\nPROGRAM(target_tool)\nENDIF()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", '#include <cstdio>\nint main() { std::puts("42"); }\n')
        graph = self.graph("--target-platform=default-linux-aarch64", "--host-platform=default-linux-x86_64")
        host = self.node(graph, "tool/host_tool")
        nodes = {node["uid"]: node for node in graph["graph"]}
        for output in ("first.txt", "second.txt"):
            pending = list(self.node(graph, output)["deps"])
            visited = set()
            while pending:
                uid = pending.pop()
                if uid in visited:
                    continue
                visited.add(uid)
                pending.extend(nodes[uid]["deps"])
            self.assertIn(host["uid"], visited, output)
