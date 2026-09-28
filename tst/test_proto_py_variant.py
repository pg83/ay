import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


EMPTY_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(x.cpp)\nEND()\n"
EV_WARNING = (
    "unsupported-source: py-addressed PROTO_LIBRARY p with .ev sources is "
    "not modelled; source skipped"
)


def py_proto_files():
    files = {
        "build/scripts/cpp_proto_wrapper.py": "print('proto')\n",
        "build/scripts/gen_py_protos.py": "print('py protos')\n",
        "contrib/libs/protobuf/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "ADDINCL(GLOBAL contrib/libs/protobuf/src)\n"
            "SRCS(protobuf.cpp)\nEND()\n"
        ),
        "contrib/libs/protobuf/protobuf.cpp": "int protobuf(){return 0;}\n",
        "contrib/libs/protobuf/src/google/protobuf/descriptor.proto": (
            'syntax = "proto2";\n'
        ),
        "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h": (
            "#pragma once\n"
        ),
        "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h": (
            "#pragma once\n"
        ),
    }
    for header in (
        "arena.h", "arenastring.h", "extension_set.h",
        "generated_message_reflection.h", "generated_message_util.h",
        "io/coded_stream.h", "message.h", "metadata_lite.h", "port_def.inc",
        "port_undef.inc", "repeated_field.h", "unknown_field_set.h",
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h",
        "reflection_ops.h", "io/printer.h", "io/zero_copy_sink.h",
        "stubs/hash.h", "stubs/stringpiece.h", "stubs/strutil.h",
        "wire_format.h",
    ):
        files[f"contrib/libs/protobuf/src/google/protobuf/{header}"] = "#pragma once\n"
    for path in (
        "contrib/libs/python", "contrib/python/protobuf",
        "contrib/tools/python3/Modules/_sqlite", "kernel/gazetteer/proto",
        "library/cpp/eventlog", "library/cpp/malloc/jemalloc",
        "library/cpp/resource", "library/python/import_tracing/constructor",
        "library/python/runtime_py3/main", "library/python/testing/import_test",
    ):
        files[f"{path}/ya.make"] = EMPTY_LIBRARY
        files[f"{path}/x.cpp"] = "int x(){return 0;}\n"
    for path in (
        "contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide",
        "contrib/python/mypy-protobuf/bin/protoc-gen-mypy",
        "dict/gazetteer/converter", "tools/archiver", "tools/event2cpp",
        "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler",
        "tools/rescompressor",
    ):
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    files["py/ya.make"] = "PY3_PROGRAM()\nPEERDIR(p q)\nPY_SRCS(m.py)\nEND()\n"
    files["py/m.py"] = "import os\n"
    return files


def make_keep_going(files, target):
    with tempfile.TemporaryDirectory(prefix="ay-make-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        value = 'OPENSOURCE = "yes"\n'
        (root / "ya.conf").write_text(
            f"[flags]\n{value}\n[host_platform_flags]\n{value}"
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing", "-k",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=10,
            check=False,
        )
        if result.returncode != 0:
            raise AssertionError(result.stderr)
        return json.loads(result.stdout), result.stderr


def link_args(graph):
    link = lib.node_by_output(graph, "$(B)/py/py")
    for cmd in link["cmds"]:
        if "$(S)/build/scripts/link_exe.py" in cmd["cmd_args"]:
            return link, cmd["cmd_args"]
    raise AssertionError("link command not found")


def whole_archive_libs(args):
    return [args[i + 1] for i, arg in enumerate(args) if arg == "--whole-archive-libs"]


class ProtoPyVariantTest(unittest.TestCase):
    def test_python_variant_links_cpp_sibling_as_whole_archive(self):
        files = py_proto_files()
        files.update({
            "p/ya.make": "PROTO_LIBRARY()\nSRCS(a.proto)\nEND()\n",
            "p/a.proto": 'syntax = "proto3";\nmessage A {}\n',
            "q/ya.make": (
                "PROTO_LIBRARY(qname)\nEXCLUDE_TAGS(CPP_PROTO)\nSRCS(q.proto)\nEND()\n"
            ),
            "q/q.proto": 'syntax = "proto3";\nmessage Q {}\n',
        })
        graph = lib.make(files, "py")

        link, args = link_args(graph)
        self.assertEqual(whole_archive_libs(args), ["q/libq.a", "p/libp.a"])
        start = args.index("--ya-start-command-file")
        end = args.index("--ya-end-command-file")
        self.assertEqual(args[start + 1:end], [
            "p/libpy3p.global.a", "q/libpy3qname.global.a", "py/libpy3py.global.a",
        ])
        self.assertIn("$(B)/p/libp.a", link["inputs"])
        self.assertIn("$(B)/p/libpy3p.global.a", link["inputs"])
        self.assertIn("$(B)/q/libpy3qname.global.a", link["inputs"])
        self.assertNotIn("$(B)/q/libq.a", link["inputs"])
        self.assertFalse(
            [n for n in graph["graph"] if "$(B)/q/q.pb.h" in n["outputs"]]
        )

        cpp_archive = lib.node_by_output(graph, "$(B)/p/libp.a")
        self.assertIn(cpp_archive["uid"], link["deps"])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/p/a.pb.h")["kv"]["p"], "PB"
        )
        py_pb = lib.node_by_output(graph, "$(B)/p/a__intpy3___pb2.py")
        self.assertEqual(py_pb["kv"]["p"], "PB")

    def test_python_variant_skips_event_and_gazetteer_sources(self):
        files = py_proto_files()
        files["py/ya.make"] = "PY3_PROGRAM()\nPEERDIR(p)\nPY_SRCS(m.py)\nEND()\n"
        files.update({
            "p/ya.make": "PROTO_LIBRARY()\nSRCS(a.proto e.ev g.gztproto)\nEND()\n",
            "p/a.proto": 'syntax = "proto3";\nmessage A {}\n',
            "p/e.ev": "message TEvent {\n}\n",
            "p/g.gztproto": "message G {}\n",
        })
        graph, stderr = make_keep_going(files, "py")

        self.assertEqual(stderr, f"\x1b[33m{EV_WARNING}\x1b[0m\n")
        py_outputs = [
            output
            for node in graph["graph"]
            for output in node["outputs"]
            if "__intpy3___pb2" in output and not output.endswith(".yapyc3")
        ]
        self.assertEqual(py_outputs, [
            "$(B)/p/a__intpy3___pb2.py", "$(B)/p/a__intpy3___pb2.pyi",
        ])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/p/e.ev.pb.h")["kv"]["p"], "EV"
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/p/g.proto")["kv"]["p"], "GZ"
        )
        cpp_archive = lib.node_by_output(graph, "$(B)/p/libp.a")
        self.assertEqual([i for i in cpp_archive["inputs"] if i.endswith(".o")], [
            "$(B)/p/a.pb.cc.o", "$(B)/p/e.ev.pb.cc.o", "$(B)/p/g.pb.cc.o",
        ])
        _, args = link_args(graph)
        self.assertEqual(whole_archive_libs(args), ["p/libp.a"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
