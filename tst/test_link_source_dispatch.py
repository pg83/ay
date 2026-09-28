import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
SOURCES = (
    "h.h a.S b.s c.asm d.cu e.gztproto f.fbs g.fbs64 h.go i.y "
    "j.fml k.sfdl l.asp m.l n.sc o.cfgproto x.cpp"
)
TOOLS = [
    "contrib/libs/flatbuffers/flatc",
    "contrib/libs/flatbuffers64/flatc",
    "contrib/tools/bison",
    "contrib/tools/flex-old",
    "contrib/tools/m4",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "contrib/tools/yasm",
    "dict/gazetteer/converter",
    "library/cpp/proto_config/codegen",
    "library/cpp/proto_config/plugin",
    "tools/calcstaticopt",
    "tools/custom_pid",
    "tools/domschemec",
    "tools/html2cpp",
    "tools/mtime0",
    "tools/relev_fml_codegen",
]
LIBRARIES = [
    "build/induced/by_bison",
    "contrib/libs/flatbuffers",
    "contrib/libs/flatbuffers64",
    "contrib/libs/protobuf",
    "kernel/gazetteer/proto",
    "library/cpp/domscheme",
    "library/cpp/proto_config/protos",
]
PLAIN_FILES = [
    "build/scripts/cpp_proto_wrapper.py",
    "build/scripts/preprocess.py",
    "util/system/compiler.h",
] + [
    f"contrib/libs/protobuf/src/google/protobuf/{name}"
    for name in (
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h", "reflection_ops.h",
    )
] + [
    f"contrib/tools/bison/data/{name}"
    for name in (
        "m4sugar/foreach.m4", "m4sugar/m4sugar.m4", "skeletons/bison.m4",
        "skeletons/c++-skel.m4", "skeletons/c++.m4", "skeletons/c-like.m4",
        "skeletons/c-skel.m4", "skeletons/c.m4", "skeletons/glr.cc",
        "skeletons/lalr1.cc", "skeletons/location.cc", "skeletons/stack.hh",
        "skeletons/variant.hh", "skeletons/yacc.c",
    )
]


def fixture(sources=SOURCES):
    files = {
        "m/ya.make": f"LIBRARY()\n{NO_PLATFORM}SRCS({sources})\nEND()\n",
        "m/x.cpp": "int x;\n",
    }
    for source in sources.split():
        files.setdefault(f"m/{source}", "\n")
    for path in TOOLS:
        lib.tool_program(files, path, path.split("/")[-1])
    for path in LIBRARIES:
        files[f"{path}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}END()\n"
    for path in PLAIN_FILES:
        files[path] = "\n"
    return files


def make_raw(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-link-sources-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
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
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


class SourceDispatchTest(unittest.TestCase):
    def test_every_source_class_reaches_its_emitter(self):
        graph = lib.make(fixture(), "m")
        module_nodes = [
            (node["kv"]["p"], node["outputs"])
            for node in graph["graph"]
            if node["outputs"][0].startswith("$(B)/m/")
        ]
        self.assertEqual(
            module_nodes,
            [
                ("GZ", ["$(B)/m/e.proto"]),
                ("FL", ["$(B)/m/f.fbs.h", "$(B)/m/f.fbs.cpp", "$(B)/m/f.bfbs"]),
                ("FL64", ["$(B)/m/g.fbs64.h", "$(B)/m/g.fbs64.cpp", "$(B)/m/g.bfbs64"]),
                ("YC", ["$(B)/m/i.h", "$(B)/m/i.y.cpp"]),
                ("HT", ["$(B)/m/l.asp.cpp"]),
                ("LX", ["$(B)/m/m.l.cpp"]),
                ("PB", ["$(B)/m/o.cfgproto.pb.cc", "$(B)/m/o.cfgproto.pb.h"]),
                ("PB", ["$(B)/m/e.pb.h", "$(B)/m/e.pb.cc"]),
                ("CC", ["$(B)/m/f.fbs.cpp.o"]),
                ("CC", ["$(B)/m/g.fbs64.cpp.o"]),
                ("CC", ["$(B)/m/i.y.cpp.o"]),
                ("CC", ["$(B)/m/l.asp.cpp.o"]),
                ("CC", ["$(B)/m/m.l.cpp.o"]),
                ("CC", ["$(B)/m/o.cfgproto.pb.cc.o"]),
                ("CC", ["$(B)/m/e.pb.cc.o"]),
                ("AS", ["$(B)/m/a.S.o"]),
                ("AS", ["$(B)/m/b.s.o"]),
                ("AS", ["$(B)/m/c.o"]),
                ("CU", ["$(B)/m/d.cu.o"]),
                ("CC", ["$(B)/m/x.cpp.o"]),
                ("AR", ["$(B)/m/libm.a"]),
            ],
        )
        produced = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertNotIn("$(B)/m/h.go.o", produced)
        self.assertNotIn("$(B)/m/h.h.o", produced)

    def test_unknown_extension_is_fatal_unless_keep_going(self):
        files = fixture("x.cpp p.CC")
        result = make_raw(files, "m")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            'unsupported-source: m: unsupported source extension in "p.CC"',
            result.stderr,
        )
        result = make_raw(files, "m", "-k")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            'unsupported-source: m: unsupported source extension in "p.CC"',
            result.stderr,
        )
        graph = json.loads(result.stdout)
        archive = lib.node_by_output(graph, "$(B)/m/libm.a")
        self.assertEqual(archive["inputs"][0], "$(B)/m/x.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
