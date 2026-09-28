import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
AARCH64 = "default-linux-aarch64"
X86_64 = "default-linux-x86_64"
OPENSOURCE_CONF = '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
INTERNAL_CONF = "[flags]\n\n[host_platform_flags]\n"
DEFAULT_PEERS = [
    "build/cow/on",
    "build/platform/clang",
    "build/platform/clang/clang-format",
    "build/platform/linux_sdk",
    "build/platform/lld",
    "build/platform/python/ymake_python3",
    "contrib/libs/asmlib",
    "contrib/libs/cxxsupp/libcxx",
    "contrib/libs/cxxsupp/libcxxrt",
    "contrib/libs/glibcasm",
    "contrib/libs/libm",
    "contrib/libs/libunwind",
    "contrib/libs/linux-headers",
    "contrib/libs/musl",
    "contrib/libs/musl/full",
    "contrib/libs/musl/include",
    "contrib/libs/tcmalloc",
    "contrib/libs/tcmalloc/default",
    "contrib/libs/tcmalloc/no_percpu_cache",
    "library/cpp/cpuid_check",
    "library/cpp/malloc/api",
    "library/cpp/malloc/jemalloc",
    "library/cpp/malloc/tcmalloc",
    "library/cpp/sanitizer/include",
    "util",
]
TOOLCHAIN = [
    "build/platform/clang",
    "build/platform/clang/clang-format",
    "build/platform/lld",
    "build/platform/python/ymake_python3",
]
RUNTIME = ["contrib/libs/cxxsupp/libcxx", "contrib/libs/cxxsupp/libcxxrt", "contrib/libs/libunwind"]
HEADERS = ["contrib/libs/linux-headers"]
SANITIZER = ["library/cpp/sanitizer/include"]
COW = ["build/cow/on"]
TCMALLOC = ["library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/no_percpu_cache"]
CPUID = ["library/cpp/cpuid_check"]
GLIBCASM = ["contrib/libs/glibcasm"]
ASMLIB = ["contrib/libs/asmlib"]
LIBRARY_DEFAULTS = TOOLCHAIN + HEADERS + RUNTIME + ["util"] + SANITIZER


def stub_tree(program_body):
    files = {
        "a/ya.make": f"PROGRAM()\n{program_body}\nSRCS(x.cpp)\nEND()\n",
        "a/x.cpp": "int main(){return 0;}\n",
    }
    for peer in DEFAULT_PEERS:
        files[f"{peer}/ya.make"] = "LIBRARY()\nNO_PLATFORM()\nSRCS(stub.cpp)\nEND()\n"
        files[f"{peer}/stub.cpp"] = "int stub;\n"
    return files


def ay_make(files, *args, platform=X86_64, conf=OPENSOURCE_CONF):
    with tempfile.TemporaryDirectory(prefix="ay-yamake-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(conf)
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
                "--target-platform", platform,
                "--host-platform", X86_64,
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


def linked_peers(graph):
    node = lib.node_by_output(graph, "$(B)/a/a")
    return [
        path[len("$(B)/"):path.rindex("/")]
        for path in node["inputs"]
        if path.endswith(".a")
    ]


class YaMakeDefaultPeersTest(unittest.TestCase):
    def peers(self, body, *args, platform=X86_64, conf=OPENSOURCE_CONF, files=None):
        tree = stub_tree(body)
        tree.update(files or {})
        code, graph, stderr = ay_make(tree, *args, platform=platform, conf=conf)
        self.assertEqual(code, 0, stderr)
        return linked_peers(graph)

    def test_program_defaults_depend_on_architecture(self):
        self.assertEqual(self.peers("", platform=AARCH64), LIBRARY_DEFAULTS + COW)
        self.assertEqual(self.peers(""), LIBRARY_DEFAULTS + COW + TCMALLOC + GLIBCASM + CPUID)

    def test_module_flags_and_platform_flags_shape_defaults(self):
        cases = [
            ("NO_UTIL()", (), TOOLCHAIN + HEADERS + RUNTIME + SANITIZER + COW + TCMALLOC + GLIBCASM),
            ("NO_RUNTIME()", (), TOOLCHAIN + HEADERS + COW + TCMALLOC + GLIBCASM),
            ("NO_PLATFORM()", (), TOOLCHAIN + HEADERS + COW + TCMALLOC),
            ("NO_LIBC()", (), TOOLCHAIN + HEADERS + COW + TCMALLOC),
            ("DISABLE(USE_ASMLIB)", (), LIBRARY_DEFAULTS + COW + TCMALLOC + CPUID),
            ("", ("-DSANITIZER_TYPE=address",), LIBRARY_DEFAULTS + COW + TCMALLOC + ASMLIB + CPUID),
            ("", ("-DUSE_SSE4=no",), LIBRARY_DEFAULTS + COW + TCMALLOC + ASMLIB + CPUID),
            ("", ("-DUSE_ARCADIA_COMPILER_RUNTIME=no",), TOOLCHAIN + HEADERS + RUNTIME + ["util"] + COW + TCMALLOC + GLIBCASM + CPUID),
            ("", ("-DUSE_ARCADIA_LIBM=yes",), LIBRARY_DEFAULTS + ["contrib/libs/libm"] + COW + TCMALLOC + GLIBCASM + CPUID),
            ("", ("--musl",), TOOLCHAIN + HEADERS + RUNTIME + ["util", "contrib/libs/musl/include"] + SANITIZER + COW + TCMALLOC + ASMLIB + ["contrib/libs/musl/full"] + CPUID),
            ("ENABLE(MUSL_LITE)", ("--musl",), TOOLCHAIN + HEADERS + RUNTIME + ["contrib/libs/musl/include"] + SANITIZER + COW + TCMALLOC + ASMLIB + ["contrib/libs/musl"]),
            ("", ("-DMUSL_LITE=yes", "--musl"), TOOLCHAIN + HEADERS + RUNTIME + ["util", "contrib/libs/musl/include"] + SANITIZER + COW + TCMALLOC + ASMLIB + ["contrib/libs/musl"] + CPUID),
        ]
        for body, args, expected in cases:
            with self.subTest(body=body, args=args):
                self.assertEqual(self.peers(body, *args), expected)
        self.assertEqual(
            self.peers("", "--musl", platform=AARCH64),
            TOOLCHAIN + HEADERS + RUNTIME + ["util", "contrib/libs/musl/include"] + SANITIZER + COW + TCMALLOC + ["contrib/libs/musl/full"],
        )

    def test_allocators_replace_the_default_allocator(self):
        cases = [
            ("ALLOCATOR(FAKE)", LIBRARY_DEFAULTS + COW + GLIBCASM),
            ("ALLOCATOR(J)", LIBRARY_DEFAULTS + COW + ["library/cpp/malloc/jemalloc"] + GLIBCASM + CPUID),
            ("ALLOCATOR(TCMALLOC)", LIBRARY_DEFAULTS + COW + ["library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/default"] + GLIBCASM + CPUID),
        ]
        for body, expected in cases:
            with self.subTest(body=body):
                self.assertEqual(self.peers(body), expected)

    def test_internal_build_adds_the_sdk_peer(self):
        self.assertEqual(
            self.peers("", conf=INTERNAL_CONF),
            TOOLCHAIN + ["build/platform/linux_sdk"] + HEADERS + RUNTIME + ["util"] + SANITIZER + COW + TCMALLOC + GLIBCASM + CPUID,
        )

    def test_library_defaults_reach_the_program_through_peers(self):
        self.assertEqual(
            self.peers(
                "NO_PLATFORM()\nPEERDIR(l)",
                platform=AARCH64,
                files={
                    "l/ya.make": "LIBRARY()\nSRCS(l.cpp)\nEND()\n",
                    "l/l.cpp": "int l;\n",
                },
            ),
            TOOLCHAIN + HEADERS + COW + RUNTIME + ["util"] + SANITIZER + ["l"],
        )

    def test_host_tools_follow_the_target_compiler_runtime_switch(self):
        for args, expected in (
            ((), LIBRARY_DEFAULTS + COW + TCMALLOC + CPUID),
            (("-DUSE_ARCADIA_COMPILER_RUNTIME=no",), TOOLCHAIN + HEADERS + RUNTIME + ["util"] + COW + TCMALLOC + CPUID),
        ):
            with self.subTest(args=args):
                tree = stub_tree("NO_PLATFORM()\nRUN_PROGRAM(tools/gen OUT gen.cpp)")
                tree["tools/gen/ya.make"] = "PROGRAM()\nSRCS(main.cpp)\nEND()\n"
                tree["tools/gen/main.cpp"] = "int main(){return 0;}\n"
                code, graph, stderr = ay_make(tree, *args, platform=AARCH64)
                self.assertEqual(code, 0, stderr)
                tool = lib.node_by_output(graph, "$(B)/tools/gen/gen")
                self.assertEqual([
                    path[len("$(B)/"):path.rindex("/")]
                    for path in tool["inputs"] if path.endswith(".a")
                ], expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
