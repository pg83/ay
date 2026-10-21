import base64
import unittest

import lib
from test_go_package import go_module, go_world
from test_resource_objcopy import base_files, objcopy_nodes, section


class ResourceVariablesTest(unittest.TestCase):
    def test_assignment_captures_defined_scalars_and_clears_unbound_ones(self):
        files = base_files(
            "SET(VALUE before)\n"
            "SET(ALIAS ${VALUE})\n"
            "SET(FORWARD ${LATER})\n"
            "SET(VALUE after)\n"
            "SET(LATER late)\n"
            "RESOURCE(- alias=${ALIAS} - forward=${FORWARD})\n"
        )
        graph = lib.make(files, "lib", "--sandboxing")
        [node] = objcopy_nodes(graph)
        self.assertEqual(section(node["cmds"][0]["cmd_args"], "--kvs"), [
            "alias=before", "forward=",
        ])

    def test_deferred_root_aliases_keep_their_resource_packing_kind(self):
        files = base_files(
            "RUN_PROGRAM(tools/gen OUT_NOAUTO generated.h)\n"
            "RESOURCE(${BINDIR}/generated.h bindir)\n"
            "RESOURCE(${ARCADIA_BUILD_ROOT}/lib/generated.h explicit)\n"
            "SET(BUILD_DIR ${BINDIR})\n"
            "RESOURCE(${BUILD_DIR}/generated.h build_alias)\n"
            "SET(SOURCE_DIR ${CURDIR})\n"
            "RESOURCE(${SOURCE_DIR}/data.txt source_alias)\n"
        )
        files["lib/data.txt"] = "data"
        lib.tool_program(files, "tools/rorescompiler", "rorescompiler")
        graph = lib.make(files, "lib", "--sandboxing")
        by_key = {}
        for node in objcopy_nodes(graph):
            args = node["cmds"][0]["cmd_args"]
            [key] = [base64.b64decode(value).decode() for value in section(args, "--keys")]
            by_key[key] = (node, section(args, "--inputs"))

        self.assertEqual(set(by_key), {"bindir", "build_alias", "source_alias"})
        producer = lib.node_by_output(graph, "$(B)/lib/generated.h")
        for key in ("bindir", "build_alias"):
            node, inputs = by_key[key]
            self.assertEqual(inputs, ["$(B)/lib/generated.h"])
            self.assertIn(producer["uid"], node["deps"])
        self.assertEqual(by_key["source_alias"][1], ["$(S)/lib/data.txt"])
        self.assertTrue(any(
            node["kv"]["p"] == "PR" and "explicit" in " ".join(node["cmds"][0]["cmd_args"])
            for node in graph["graph"]
        ))

    def test_native_resources_expand_paths_keys_and_values_before_packing(self):
        declaration = "RESOURCE(${DATA_DIR}/data.txt ${KEY_NAME} - label=${VALUE})\n"
        setup = (
            "SET(DATA_DIR ${ARCADIA_ROOT}/assets)\n"
            "SET(KEY_NAME /assets/data)\nSET(VALUE payload)\n"
        )
        files = base_files(setup + declaration)
        files["assets/data.txt"] = "payload"
        graph = lib.make(files, "lib", "--sandboxing")
        [packed] = objcopy_nodes(graph)
        args = packed["cmds"][0]["cmd_args"]
        self.assertEqual(section(args, "--inputs"), ["$(S)/assets/data.txt"])
        self.assertEqual([base64.b64decode(k).decode() for k in section(args, "--keys")], ["/assets/data"])
        self.assertEqual(section(args, "--kvs"), ["label=payload"])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.global.a")
        self.assertIn(packed["uid"], archive["deps"])
        self.assertIn(packed["outputs"][0], archive["inputs"])
        files["lib/ya.make"] = files["lib/ya.make"].replace(
            declaration, "RESOURCE(${ARCADIA_ROOT}/assets/data.txt /assets/data - label=payload)\n"
        )
        [literal] = objcopy_nodes(lib.make(files, "lib", "--sandboxing"))
        self.assertEqual(packed["outputs"], literal["outputs"])
        self.assertEqual(packed["cmds"], literal["cmds"])

    def test_shared_resource_ast_uses_each_module_environment(self):
        files = base_files("")
        files["shared/resource.inc"] = "RESOURCE(${DATA_DIR}/data.txt ${KEY_NAME})\n"
        for name in ("first", "second"):
            files[f"{name}/ya.make"] = (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                f"SET(DATA_DIR ${{ARCADIA_ROOT}}/assets/{name})\nSET(KEY_NAME /{name})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/resource.inc)\nEND()\n"
            )
            files[f"assets/{name}/data.txt"] = name
        for targets in (("first", "second"), ("second", "first")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, "--sandboxing")
                for name in ("first", "second"):
                    [packed] = [n for n in graph["graph"] if any(o.startswith(f"$(B)/{name}/objcopy_") for o in n["outputs"])]
                    args = packed["cmds"][0]["cmd_args"]
                    self.assertEqual(section(args, "--inputs"), [f"$(S)/assets/{name}/data.txt"])
                    self.assertEqual([base64.b64decode(k).decode() for k in section(args, "--keys")], [f"/{name}"])

    def test_go_resources_expand_the_same_arguments_before_hashing(self):
        files = go_world()
        go_module(files, "library/go/core/resource")
        go_module(files, "library/go/core/resource/cc", module="GO_PROGRAM")
        declaration = "RESOURCE(${DATA_DIR}/data.txt ${KEY_NAME})\n"
        go_module(files, "assets", macros="SET(DATA_DIR ${ARCADIA_ROOT}/payload)\nSET(KEY_NAME /assets/data)\n" + declaration)
        files["payload/data.txt"] = "payload"
        graph = lib.make(files, "assets", "--sandboxing")
        [packed] = [n for n in graph["graph"] if any(o.endswith(".res.go") for o in n["outputs"])]
        self.assertIn("$(S)/payload/data.txt", packed["inputs"])
        self.assertEqual(packed["cmds"][0]["cmd_args"][-2:], ["$(S)/payload/data.txt", "notafile/assets/data"])
        compiler = lib.node_by_output(graph, "$(B)/library/go/core/resource/cc/cc")
        self.assertIn(compiler["uid"], packed["deps"])
        files["assets/ya.make"] = files["assets/ya.make"].replace(
            declaration, "RESOURCE(${ARCADIA_ROOT}/payload/data.txt /assets/data)\n"
        )
        literal_graph = lib.make(files, "assets", "--sandboxing")
        [literal] = [n for n in literal_graph["graph"] if any(o.endswith(".res.go") for o in n["outputs"])]
        self.assertEqual(packed["outputs"], literal["outputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
