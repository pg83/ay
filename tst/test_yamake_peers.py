import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
LIBRARIES = [
    "contrib/libs/googleapis-common-protos",
    "contrib/libs/grpc",
    "contrib/libs/protobuf",
    "contrib/libs/python",
    "dl",
    "kernel/struct_codegen/metadata",
    "kernel/struct_codegen/reflection",
    "library/cpp/domscheme",
    "library/cpp/eventlog",
    "library/cpp/proto_config/codegen",
    "library/cpp/proto_config/protos",
    "library/cpp/resource",
    "library/python/runtime_py3",
    "maps/libs/sproto",
    "tools/enum_parser/enum_serialization_runtime",
]
TOOLS = [
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "kernel/struct_codegen/codegen_tool",
    "library/cpp/proto_config/plugin",
    "maps/libs/sproto/sprotoc",
    "tools/domschemec",
    "tools/enum_parser/enum_parser",
    "tools/event2cpp",
    "tools/rescompiler",
    "tools/rescompressor",
]


def tree(body, sources):
    files = {
        "a/ya.make": "PROGRAM()\n" + NO_PLATFORM + body + "\nSRCS(main.cpp)\nEND()\n",
        "a/main.cpp": "int main(){return 0;}\n",
    }
    for source in sources:
        files[f"a/{source}"] = "\n"
    for path in LIBRARIES:
        files[f"{path}/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "SRCS(stub.cpp)\nEND()\n"
        files[f"{path}/stub.cpp"] = "int stub;\n"
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


def ay_make(files, *args):
    with tempfile.TemporaryDirectory(prefix="ay-yamake-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\n'
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
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, "a",
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


class YaMakeImplicitPeersTest(unittest.TestCase):
    def test_sources_and_macros_add_implicit_peers(self):
        cases = [
            ("SRCS(x.ev)", ["x.ev"], ["library/cpp/eventlog", "contrib/libs/protobuf"]),
            ("SRCS(y.sc)", ["y.sc"], ["library/cpp/domscheme"]),
            ("SRCS(z.cfgproto)", ["z.cfgproto"], ["library/cpp/proto_config/codegen", "library/cpp/proto_config/protos", "contrib/libs/protobuf"]),
            ("SRCS(p.proto)", ["p.proto"], ["contrib/libs/protobuf"]),
            ("GRPC()\nUSE_COMMON_GOOGLE_APIS()", [], ["contrib/libs/googleapis-common-protos", "contrib/libs/grpc"]),
            ("GENERATE_ENUM_SERIALIZATION(e.h)", ["e.h"], ["tools/enum_parser/enum_serialization_runtime"]),
            ("RESOURCE(r.txt /key)", ["r.txt"], ["library/cpp/resource"]),
            ("YMAPS_SPROTO(y.proto)", ["y.proto"], ["maps/libs/sproto"]),
            ("DYNAMIC_LIBRARY_FROM(dl)", [], ["dl"]),
            ("STRUCT_CODEGEN(sc)", ["sc.in"], ["kernel/struct_codegen/metadata", "kernel/struct_codegen/reflection"]),
            ("USE_PYTHON3()", [], ["contrib/libs/python", "library/python/runtime_py3"]),
        ]
        for body, sources, expected in cases:
            with self.subTest(body=body):
                code, graph, stderr = ay_make(tree(body, sources), "-k")
                self.assertEqual(code, 0, stderr)
                linked = [
                    path[len("$(B)/"):path.rindex("/")]
                    for path in lib.node_by_output(graph, "$(B)/a/a")["inputs"]
                    if path.endswith(".a")
                ]
                self.assertEqual(linked, expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
