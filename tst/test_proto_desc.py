import unittest

import lib


PYTHON = "$(B)/resources/YMAKE_PYTHON3/bin/python3"
PROTOC = "$(B)/contrib/tools/protoc/protoc"
DESC_WRAPPER = "$(S)/build/scripts/desc_rawproto_wrapper.py"
MERGE_FILES = "$(S)/build/scripts/merge_files.py"
COLLECT_RAWPROTO = "$(S)/build/scripts/collect_rawproto.py"
MERGE_PROTOSRC = "$(S)/build/scripts/merge_protosrc.py"
BUILTIN = "contrib/libs/protobuf/builtin_proto/protos_from_protoc"
BUILTIN_DESC = f"$(B)/{BUILTIN}/protobuf-builtin_proto-protos_from_protoc.self.protodesc"
LEAF_HASH = "bab4ff04cc14af66e4d42c85f888cfe6"
MID_HASH = "22384709d743fe3c6fb0a4b35b2e10a6"


def desc_files():
    files = {
        "build/scripts/desc_rawproto_wrapper.py": "print('desc')\n",
        "build/scripts/merge_files.py": "print('merge')\n",
        "build/scripts/collect_rawproto.py": "print('collect')\n",
        "build/scripts/merge_protosrc.py": "print('protosrc')\n",
        "contrib/libs/protobuf/src/google/protobuf/descriptor.proto": (
            'syntax = "proto2";\n'
        ),
        f"{BUILTIN}/ya.make": (
            "PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL contrib/libs/protobuf/src)\n"
            "SRCDIR(contrib/libs/protobuf/src)\n"
            "SRCS(google/protobuf/descriptor.proto)\nEND()\n"
        ),
    }
    lib.tool_program(files, "contrib/tools/protoc", "protoc")
    return files


def desc_node(graph, output):
    node = lib.node_by_output(graph, output)
    assert node["kv"] == {"p": "PD", "pc": "light-cyan"}, node["kv"]
    return node


class ProtoDescTest(unittest.TestCase):
    def test_descriptions_merge_proto_library_closure(self):
        files = desc_files()
        files.update({
            "leaf/ya.make": (
                "PROTO_LIBRARY()\nPROTO_NAMESPACE(leaf)\n"
                "ADDINCL(GLOBAL FOR proto leaf/extra)\n"
                "SRCS(leaf.proto)\nEND()\n"
            ),
            "leaf/leaf.proto": 'syntax = "proto3";\nmessage L {}\n',
            "leaf/extra/.keep": "",
            "mid/ya.make": (
                "PROTO_LIBRARY()\nPEERDIR(leaf)\nSRCS(m.proto events.ev n.proto)\n"
                "END()\n"
            ),
            "mid/m.proto": (
                'syntax = "proto3";\nimport "leaf.proto";\n'
                'import "google/protobuf/descriptor.proto";\nmessage M {}\n'
            ),
            "mid/n.proto": 'syntax = "proto3";\nmessage N {}\n',
            "mid/events.ev": "message TEvent {}\n",
            "plain/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(x.cpp)\nEND()\n"
            ),
            "plain/x.cpp": "int x(){return 0;}\n",
            "aggregate/ya.make": "RECURSE(leaf)\n",
            "d/ya.make": (
                "PROTO_DESCRIPTIONS()\n"
                "PEERDIR(mid leaf plain aggregate missing)\nEND()\n"
            ),
        })
        graph = lib.make(files, "d")

        leaf_desc = desc_node(graph, "$(B)/leaf/leaf.proto.desc")
        leaf_raw = f"$(B)/leaf/leaf.proto.{LEAF_HASH}.rawproto"
        self.assertEqual(leaf_desc["outputs"], ["$(B)/leaf/leaf.proto.desc", leaf_raw])
        self.assertEqual(leaf_desc["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(leaf_desc["cmds"][0]["cmd_args"], [
            PYTHON, DESC_WRAPPER, "--desc-output", "$(B)/leaf/leaf.proto.desc",
            "--rawproto-output", leaf_raw, "--proto-file", "leaf/leaf.proto",
            "--", PROTOC, "-I=./leaf", "-I=$(S)/leaf", "-I=$(B)", "-I=$(S)",
            "-I=$(S)/leaf", "-I=$(S)/contrib/libs/protobuf/src", "-I=$(B)",
            "-I=$(S)/contrib/libs/protobuf/src", "--include_source_info",
        ])
        self.assertEqual(
            leaf_desc["inputs"], [PROTOC, "$(S)/leaf/leaf.proto", DESC_WRAPPER]
        )
        self.assertIn(lib.node_by_output(graph, PROTOC)["uid"], leaf_desc["deps"])

        m_desc = desc_node(graph, "$(B)/mid/m.proto.desc")
        self.assertEqual(m_desc["cmds"][0]["cmd_args"][9:], [
            PROTOC, "-I=./", "-I=$(S)/", "-I=$(B)", "-I=$(S)",
            "-I=$(S)/contrib/libs/protobuf/src", "-I=$(S)/leaf",
            "-I=$(S)/leaf/extra", "-I=$(B)",
            "-I=$(S)/contrib/libs/protobuf/src", "--include_source_info",
        ])
        self.assertEqual(
            m_desc["inputs"][:3], [PROTOC, "$(S)/mid/m.proto", DESC_WRAPPER]
        )
        self.assertEqual(set(m_desc["inputs"][3:]), {
            "$(S)/leaf/leaf.proto",
            "$(S)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto",
        })

        mid_merge = desc_node(graph, "$(B)/mid/mid.self.protodesc")
        self.assertEqual(
            mid_merge["outputs"], ["$(B)/mid/mid.self.protodesc", "$(B)/mid/mid.protosrc"]
        )
        merge_cmd, collect_cmd = mid_merge["cmds"]
        self.assertNotIn("cwd", merge_cmd)
        self.assertEqual(merge_cmd["cmd_args"], [
            PYTHON, MERGE_FILES, "$(B)/mid/mid.self.protodesc",
            "$(B)/mid/m.proto.desc", "$(B)/mid/n.proto.desc",
        ])
        self.assertEqual(collect_cmd["cwd"], "$(B)")
        self.assertEqual(collect_cmd["cmd_args"], [
            PYTHON, COLLECT_RAWPROTO, "--output", "$(B)/mid/mid.protosrc",
            f"mid/m.proto.{MID_HASH}.rawproto", f"mid/n.proto.{MID_HASH}.rawproto",
        ])
        self.assertEqual(mid_merge["inputs"][:6], [
            "$(B)/mid/m.proto.desc", "$(B)/mid/n.proto.desc",
            f"$(B)/mid/m.proto.{MID_HASH}.rawproto",
            f"$(B)/mid/n.proto.{MID_HASH}.rawproto",
            DESC_WRAPPER, "$(S)/mid/m.proto",
        ])
        self.assertEqual(set(mid_merge["inputs"][6:8]), {
            "$(S)/leaf/leaf.proto",
            "$(S)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto",
        })
        self.assertEqual(
            mid_merge["inputs"][8:], ["$(S)/mid/n.proto", MERGE_FILES, COLLECT_RAWPROTO]
        )
        self.assertFalse(
            [n for n in graph["graph"] if any("events" in o for o in n["outputs"])]
        )
        self.assertEqual(sorted(mid_merge["deps"]), sorted([
            m_desc["uid"], desc_node(graph, "$(B)/mid/n.proto.desc")["uid"],
        ]))

        builtin_desc = desc_node(
            graph, f"$(B)/{BUILTIN}/__/__/src/google/protobuf/descriptor.proto.desc"
        )
        self.assertIn(
            "$(B)/contrib/libs/protobuf/src/google/protobuf/"
            "descriptor.proto.84a76baad339f38ef58b4b213bac2a15.rawproto",
            builtin_desc["outputs"],
        )
        self.assertIn(
            "contrib/libs/protobuf/src/google/protobuf/descriptor.proto",
            builtin_desc["cmds"][0]["cmd_args"],
        )

        final = desc_node(graph, "$(B)/d/d.protodesc")
        self.assertEqual(final["outputs"], ["$(B)/d/d.protodesc", "$(B)/d/d.tar"])
        closure = [BUILTIN_DESC, "$(B)/leaf/leaf.self.protodesc", "$(B)/mid/mid.self.protodesc"]
        self.assertEqual(final["cmds"][0]["cmd_args"], [
            PYTHON, MERGE_FILES, "$(B)/d/d.protodesc", *closure,
        ])
        self.assertEqual(final["cmds"][1]["cwd"], "$(B)")
        self.assertEqual(final["cmds"][1]["cmd_args"], [
            PYTHON, MERGE_PROTOSRC, "--output", "$(B)/d/d.tar",
            *[path.removeprefix("$(B)/") for path in closure],
        ])
        self.assertEqual(final["inputs"], [*closure, MERGE_FILES, MERGE_PROTOSRC])
        self.assertEqual(len(final["deps"]), 3)

    def test_disabled_builtins_and_toolchain_sbom(self):
        files = desc_files()
        files.update({
            "build/internal/conf/sbom.conf": "\n",
            "build/internal/scripts/gen_sbom.py": "print('sbom')\n",
            "build/platform/python/ymake_python3/ya.make": (
                "RESOURCES_LIBRARY()\nTOOLCHAIN(python3)\nVERSION(3.12.6)\n"
                "NO_YMAKE_PYTHON3()\n"
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON("
                "YMAKE_PYTHON3 resources.json)\nEND()\n"
            ),
            "build/platform/python/ymake_python3/resources.json": (
                '{"by_platform": {"linux-x86_64": {"uri": "sbr:1"}}}'
            ),
            "leaf/ya.make": (
                "PROTO_LIBRARY()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\n"
                "SRCS(leaf.proto)\nEND()\n"
            ),
            "leaf/leaf.proto": 'syntax = "proto3";\nmessage L {}\n',
            "d/ya.make": "PROTO_DESCRIPTIONS()\nPEERDIR(leaf)\nEND()\n",
        })
        lib.tool_program(files, "build/internal/platform/clang_toolchain_info", "info")
        graph = lib.make(
            files, "d", "--target-platform", "default-linux-x86_64"
        )
        self.assertFalse(
            [n for n in graph["graph"] if any(BUILTIN in o for o in n["outputs"])]
        )
        sbom = "$(B)/build/platform/python/ymake_python3/toolchain.component.sbom"
        final = desc_node(graph, "$(B)/d/d.protodesc")
        self.assertEqual(final["platform"], "default-linux-x86_64")
        self.assertEqual(final["inputs"], [
            "$(B)/leaf/leaf.self.protodesc", sbom, MERGE_FILES, MERGE_PROTOSRC,
        ])
        sbom_uids = {
            n["uid"] for n in graph["graph"] if sbom in n["outputs"]
        }
        self.assertTrue(sbom_uids & set(final["deps"]))
        self.assertIn(
            desc_node(graph, "$(B)/leaf/leaf.self.protodesc")["uid"], final["deps"]
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
