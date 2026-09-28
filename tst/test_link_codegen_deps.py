import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
COPIES = 17
HEADERS = 16


def fixture():
    files = {
        "m/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}"
            + "".join(f"COPY_FILE(o{i}.bin d{i}.o)\n" for i in range(COPIES))
            + "SRCS(x.cpp)\nEND()\n"
        ),
        "m/x.cpp": "int x;\n",
        "dup/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}"
            "FROM_SANDBOX(123 OUT x.h)\nFROM_SANDBOX(456 OUT x.h)\nSRCS(y.cpp)\nEND()\n"
        ),
        "dup/y.cpp": '#include "x.h"\n',
        "fs/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}FROM_SANDBOX(123 OUT blob.o)\nSRCS(y.cpp)\nEND()\n"
        ),
        "fs/y.cpp": "int y;\n",
        "hdr/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}"
            + "".join(f"COPY_FILE(h{i}.in h{i}.h)\n" for i in range(HEADERS))
            + "FROM_SANDBOX(7 OUT a.h b.h)\nSRCS(x.cpp)\nEND()\n"
        ),
        "hdr/x.cpp": "".join(f'#include "h{i}.h"\n' for i in range(HEADERS))
        + '#include "a.h"\n#include "b.h"\n',
    }
    for i in range(HEADERS):
        files[f"hdr/h{i}.in"] = f"int h{i};\n"
    files["srcdst/ya.make"] = (
        f"LIBRARY()\n{NO_PLATFORM}COPY_FILE(a.txt ${{ARCADIA_ROOT}}/srcdst/b.txt)\n"
        "SRCS(x.cpp)\nEND()\n"
    )
    files["srcdst/a.txt"] = "a\n"
    files["srcdst/x.cpp"] = "int x;\n"
    files["unproduced/ya.make"] = (
        f"LIBRARY()\n{NO_PLATFORM}"
        "SRCS(${ARCADIA_BUILD_ROOT}/unproduced/nogen.cpp x.cpp)\nEND()\n"
    )
    files["unproduced/x.cpp"] = "int x;\n"
    files["dt/ya.make"] = (
        f"DLL_TOOL(t)\n{NO_PLATFORM}EXPORTS_SCRIPT(t.exports)\n"
        + "".join(f"COPY_FILE(o{i}.bin d{i}.o)\n" for i in range(COPIES))
        + "SRCS(x.cpp)\nEND()\n"
    )
    files["dt/x.cpp"] = "int x;\n"
    files["dt/t.exports"] = "{};\n"
    for i in range(COPIES):
        files[f"dt/o{i}.bin"] = f"{i}\n"
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    for i in range(COPIES):
        files[f"m/o{i}.bin"] = f"{i}\n"
    return files


def make_raw(files, target):
    with tempfile.TemporaryDirectory(prefix="ay-link-codegen-") as directory:
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
                target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


class CodegenDepsTest(unittest.TestCase):
    def test_generated_archive_members_are_archive_deps(self):
        graph = lib.make(fixture(), "m")
        copies = [lib.node_by_output(graph, f"$(B)/m/d{i}.o") for i in range(COPIES)]
        compile_node = lib.node_by_output(graph, "$(B)/m/x.cpp.o")
        archive = lib.node_by_output(graph, "$(B)/m/libm.a")
        self.assertEqual(
            sorted(archive["deps"]),
            sorted([node["uid"] for node in copies] + [compile_node["uid"]]),
        )
        self.assertEqual(len(archive["deps"]), len(set(archive["deps"])))
        self.assertEqual(
            archive["inputs"][:COPIES + 1],
            [f"$(B)/m/d{i}.o" for i in range(COPIES)] + ["$(B)/m/x.cpp.o"],
        )

    def test_duplicate_generated_output_is_rejected(self):
        result = make_raw(fixture(), "dup")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            'CodegenRegistry: duplicate producer for "$(B)/dup/x.h" '
            "(existing ref=1, new ref=2)",
            result.stderr,
        )

    def test_from_sandbox_object_is_an_archive_member(self):
        graph = lib.make(fixture(), "fs")
        fetch = lib.node_by_output(graph, "$(B)/fs/blob.o")
        archive = lib.node_by_output(graph, "$(B)/fs/libfs.a")
        self.assertEqual(archive["inputs"][:2], ["$(B)/fs/blob.o", "$(B)/fs/y.cpp.o"])
        self.assertIn(fetch["uid"], archive["deps"])
        self.assertEqual(
            archive["cmds"][0]["cmd_args"][-2:],
            ["$(B)/fs/blob.o", "$(B)/fs/y.cpp.o"],
        )

    def test_compile_depends_once_on_each_generated_header_producer(self):
        graph = lib.make(fixture(), "hdr")
        compile_node = lib.node_by_output(graph, "$(B)/hdr/x.cpp.o")
        producers = [lib.node_by_output(graph, f"$(B)/hdr/h{i}.h")["uid"] for i in range(HEADERS)]
        sandbox = lib.node_by_output(graph, "$(B)/hdr/a.h")
        self.assertEqual(sandbox["outputs"], ["$(B)/hdr/a.h", "$(B)/hdr/b.h"])
        self.assertEqual(sorted(compile_node["deps"]), sorted(producers + [sandbox["uid"]]))
        for header in ["$(B)/hdr/a.h", "$(B)/hdr/b.h"] + [f"$(B)/hdr/h{i}.h" for i in range(HEADERS)]:
            with self.subTest(header=header):
                self.assertIn(header, compile_node["inputs"])

    def test_copy_into_source_root_is_rejected(self):
        result = make_raw(fixture(), "srcdst")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            'CodegenRegistry: register of a source path "$(S)/srcdst/b.txt"',
            result.stderr,
        )

    def test_unproduced_build_source_compiles_without_producer_dep(self):
        graph = lib.make(fixture(), "unproduced")
        node = lib.node_by_output(graph, "$(B)/unproduced/nogen.cpp.o")
        self.assertEqual(node["inputs"], ["$(B)/unproduced/nogen.cpp"])
        self.assertEqual(node["deps"], [])

    def test_shared_object_depends_once_on_generated_members(self):
        graph = lib.make(fixture(), "dt")
        node = lib.node_by_output(graph, "$(B)/dt/libt.so")
        copies = [lib.node_by_output(graph, f"$(B)/dt/d{i}.o")["uid"] for i in range(COPIES)]
        self.assertEqual(len(node["deps"]), len(set(node["deps"])))
        self.assertTrue(set(copies) <= set(node["deps"]))
        for i in range(COPIES):
            self.assertIn(f"$(B)/dt/d{i}.o", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
