import unittest

from tst import lib


TOOLS = ("grove/tool", "contrib/tools/gperf", "tools/lua", "tools/aux", "tools/rescompiler", "tools/rescompressor")


def files(kind, explicit=False):
    sources = {"library/cpp/resource/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n"}
    for tool in TOOLS:
        lib.tool_program(sources, tool, tool.rsplit("/", 1)[-1])
        sources[tool + "/ya.make"] = sources[tool + "/ya.make"].replace(
            "SRCS(main.cpp)", "ALLOCATOR(FAKE)\nSRCS(main.cpp GLOBAL stamp.cpp)")
        sources[tool + "/stamp.cpp"] = "int generator_stamp;\n"
    sources["grove/parcel/ya.make"] = "PACKAGE(parcel)\nPEERDIR(" + " ".join(TOOLS) + ")\nEND()\n"
    extra = " IN ${ARCADIA_BUILD_ROOT}/tools/aux/aux" if explicit else ""
    statements = {
        "base": "BASE_CODEGEN(grove/tool seed)\nSRCS(${BINDIR}/seed.cpp)\n",
        "gperf": "SRCS(seed.gperf)\n",
        "lua": "RUN_LUA(seed.lua OUT made.cpp)\n",
        "python": f"RUN_PYTHON3(seed.py TOOL tools/aux{extra} OUT made.cpp)\n",
        "resource": "RESOURCE(payload.txt generator_data)\n",
    }
    module = "UNION" if kind == "python" else "LIBRARY"
    peer = "PEERDIR(tools/aux)\n" if explicit else ""
    sources.update({
        "grove/consumer/ya.make": f"{module}(consumer)\n{lib.BARE_MODULE}{peer}{statements[kind]}END()\n",
        "grove/consumer/seed.in": "// input\n",
        "grove/consumer/seed.gperf": "%%\nentry\n",
        "grove/consumer/seed.lua": "-- generator\n",
        "grove/consumer/seed.py": "print('generator')\n",
        "grove/consumer/payload.txt": "payload\n",
    })
    return sources


class GeneratorToolRolesTest(unittest.TestCase):
    def check_graph(self, graph, kind, explicit):
        stamps = {node["uid"]: out for node in graph["graph"] for out in node["outputs"]
                  if any(out == "$(B)/" + tool + "/stamp.cpp.o" for tool in TOOLS)}
        self.assertTrue(stamps)
        code = {"base": "BC", "gperf": "GP", "lua": "LU", "python": "PY", "resource": "PY"}[kind]
        consumers = [node for node in graph["graph"] if node["kv"]["p"] == code
                     and any(out.startswith("$(B)/grove/consumer/") for out in node["outputs"])]
        self.assertEqual(len(consumers), 1)
        consumer = consumers[0]
        used_tools = {
            "base": ("grove/tool",), "gperf": ("contrib/tools/gperf",),
            "lua": ("tools/lua",), "python": ("tools/aux",),
            "resource": ("tools/rescompiler", "tools/rescompressor"),
        }[kind]
        for tool in used_tools:
            binary = "$(B)/" + tool + "/" + tool.rsplit("/", 1)[-1]
            self.assertIn(binary, consumer["inputs"])
            self.assertTrue(any(binary in node["outputs"] and node["uid"] in consumer["deps"]
                                for node in graph["graph"]))
        for uid, output in stamps.items():
            self.assertTrue(any(node["kv"]["p"] == "LD" and output in node["inputs"] and uid in node["deps"]
                                for node in graph["graph"]))
        if explicit:
            data = [path for path in stamps.values() if path.startswith("$(B)/tools/aux/")]
            self.assertTrue(data)
            self.assertTrue(set(data) <= set(consumer["inputs"]))
            self.assertTrue(any(uid in consumer["deps"] and path in data for uid, path in stamps.items()))
        else:
            self.assertTrue(set(consumer["inputs"]).isdisjoint(stamps.values()))
            self.assertTrue(set(consumer["deps"]).isdisjoint(stamps))
        parcel = lib.node_by_output(graph, "$(B)/grove/parcel/parcel.final.pkg.fake")
        self.assertTrue(set(stamps.values()) & set(parcel["inputs"]))
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_codegen_and_resource_tools_keep_globals_owned_by_the_tool(self):
        for kind in ("base", "gperf", "lua", "python", "resource"):
            with self.subTest(kind=kind):
                graph = lib.make(files(kind), *TOOLS, "grove/parcel", "grove/consumer", "--sandboxing")
                self.check_graph(graph, kind, False)

    def test_auxiliary_python_tool_can_also_be_an_explicit_build_input(self):
        graph = lib.make(files("python", True), *TOOLS, "grove/parcel", "grove/consumer", "--sandboxing")
        self.check_graph(graph, "python", True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
