import unittest

import lib
from test_proto_py_variant import py_proto_files
from test_python2 import python2_base


RESOURCE_TOOLS = (
    "contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide",
    "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "tools/py3cc/slow",
)


def files():
    sources = py_proto_files()
    sources.update(python2_base())
    sources.update({
        "grove/protocol/ya.make": f"PROTO_LIBRARY(messages)\n{lib.BARE_MODULE}SRCS(message.proto common.proto)\nEND()\n",
        "grove/protocol/message.proto": 'syntax = "proto2";\nimport "grove/protocol/common.proto";\nmessage Envelope { optional Metadata value = 1; }\n',
        "grove/protocol/common.proto": 'syntax = "proto2";\nmessage Metadata { optional int32 value = 1; }\n',
        "grove/app/ya.make": (
            f"PY3_PROGRAM(application)\n{lib.BARE_MODULE}ALLOCATOR(FAKE)\nNO_PYTHON_INCLUDES()\n"
            "NO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\nNO_CHECK_IMPORTS()\n"
            "PY_MAIN(main)\nPY_SRCS(TOP_LEVEL main.py)\nPEERDIR(grove/protocol)\nEND()\n"),
        "grove/native/ya.make": (
            f"PROGRAM(carrier)\n{lib.BARE_MODULE}ALLOCATOR(FAKE)\nSRCS(main.cpp)\n"
            "PEERDIR(grove/protocol)\nEND()\n"),
        "grove/native/main.cpp": "int main() { return 0; }\n",
        "grove/app/main.py": "print('ready')\n",
    })
    sources["grove/legacy/ya.make"] = (
        f"PY2_PROGRAM(legacy)\n{lib.BARE_MODULE}ALLOCATOR(FAKE)\nNO_PYTHON_INCLUDES()\n"
        "NO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\nNO_CHECK_IMPORTS()\n"
        "PY_MAIN(main)\nPY_SRCS(TOP_LEVEL main.py)\nEND()\n")
    sources["grove/legacy/main.py"] = "print('legacy')\n"
    for tool in (*RESOURCE_TOOLS, "tools/py2cc"):
        sources[tool + "/ya.make"] = sources[tool + "/ya.make"].replace(
            "END()", "ALLOCATOR(FAKE)\nRESOURCE(marker.txt generator_marker)\nEND()")
        sources[tool + "/marker.txt"] = "generator resource\n"
    return sources


class ToolIncludedDepsTest(unittest.TestCase):
    def test_proto_and_bytecode_keep_tool_edges_without_tool_included_inputs(self):
        for targets in (("grove/app", "grove/native"),
                        (*RESOURCE_TOOLS, "grove/app", "grove/native"),
                        ("grove/native", "grove/app", *RESOURCE_TOOLS)):
            with self.subTest(targets=targets):
                graph = lib.make(files(), *targets, "--sandboxing")
                objects = {node["uid"]: output for node in graph["graph"] for output in node["outputs"]
                           if any(output.startswith("$(B)/" + tool + "/objcopy_") for tool in RESOURCE_TOOLS)}
                self.assertGreaterEqual(len(set(objects.values())), len(RESOURCE_TOOLS))
                consumers = [node for node in graph["graph"] if (node["kv"].get("p") == "PB" or
                                 node["kv"].get("p") == "PY" and any(out.endswith(".yapyc3") for out in node["outputs"]))
                             and any(output.startswith("$(B)/grove/") for output in node["outputs"])]
                self.assertTrue(any(node["kv"]["p"] == "PB" and any("__intpy3__" in out for out in node["outputs"])
                                    for node in consumers))
                self.assertTrue(any(node["kv"]["p"] == "PB" and any(out.endswith(".pb.cc") for out in node["outputs"])
                                    for node in consumers))
                self.assertTrue(any(any(out.endswith(".yapyc3") for out in node["outputs"]) for node in consumers))
                for node in consumers:
                    self.assertTrue(any(tool in node["inputs"] for tool in (
                        "$(B)/contrib/tools/protoc/protoc", "$(B)/tools/py3cc/py3cc")))
                    if node["kv"]["p"] == "PB":
                        if any("message" in out for out in node["outputs"]):
                            self.assertIn("$(S)/grove/protocol/message.proto", node["inputs"])
                        self.assertIn("$(S)/grove/protocol/common.proto", node["inputs"])
                        wrapper = "gen_py_protos.py" if any("__intpy3__" in out for out in node["outputs"]) else "cpp_proto_wrapper.py"
                        self.assertIn("$(S)/build/scripts/" + wrapper, node["inputs"])
                    else:
                        built_sources = [path for path in node["inputs"]
                                         if path.startswith("$(B)/grove/") and path.endswith(".py")]
                        for path in built_sources:
                            producer = lib.node_by_output(graph, path)
                            self.assertIn(producer["uid"], node["deps"])
                    self.assertTrue(set(node["inputs"]).isdisjoint(objects.values()))
                    self.assertTrue(set(node["deps"]).isdisjoint(objects))
                for tool in RESOURCE_TOOLS:
                    linked = [node for node in graph["graph"]
                              if "$(B)/" + tool + "/" + tool.rsplit("/", 1)[-1] in node["outputs"]]
                    owned = {uid: output for uid, output in objects.items()
                             if output.rsplit("/", 1)[0] == "$(B)/" + tool}
                    self.assertTrue(owned)
                    self.assertTrue(linked)
                    for node in linked:
                        self.assertTrue(set(owned.values()) <= set(node["inputs"]))
                        self.assertTrue(any(uid in node["deps"] for uid in owned))
                ids = {node["uid"] for node in graph["graph"]}
                self.assertLessEqual(set(graph["result"]), ids)
                self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_python2_bytecode_keeps_its_resourceful_tool_without_inheriting_objects(self):
        graph = lib.make(files(), "tools/py2cc", "grove/legacy", "--sandboxing")
        objects = {node["uid"]: output for node in graph["graph"] for output in node["outputs"]
                   if output.startswith("$(B)/tools/py2cc/objcopy_")}
        self.assertTrue(objects)
        consumers = [node for node in graph["graph"]
                     if any(out.startswith("$(B)/grove/legacy/") and out.endswith(".yapyc") for out in node["outputs"])]
        self.assertTrue(consumers)
        tools = [node for node in graph["graph"] if "$(B)/tools/py2cc/py2cc" in node["outputs"]]
        for node in consumers:
            self.assertIn("$(B)/tools/py2cc/py2cc", node["inputs"])
            self.assertTrue(any(tool["uid"] in node["deps"] for tool in tools))
            self.assertTrue(set(node["inputs"]).isdisjoint(objects.values()))
            self.assertTrue(set(node["deps"]).isdisjoint(objects))
        for tool in tools:
            self.assertTrue(set(objects.values()) <= set(tool["inputs"]))
            self.assertTrue(any(uid in tool["deps"] for uid in objects))


if __name__ == "__main__":
    unittest.main(verbosity=2)
