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
EXPANSION_PASSES = 8


def module(kind, body):
    return f"{kind}()\n" + NO_PLATFORM + body + "\nEND()\n"


def ay_make(files, target, *args, platform="default-linux-aarch64"):
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
                "--target-platform", platform,
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
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


def reference_chain(name, final):
    defines = [f"-D{name}{i}=${{{name}{i + 1}}}" for i in range(1, EXPANSION_PASSES)]
    return defines + [f"-D{name}{EXPANSION_PASSES}={final}"]


class YaMakeEdgesTest(unittest.TestCase):
    def make(self, files, target="a", *args, **kwargs):
        code, graph, stderr = ay_make(files, target, *args, **kwargs)
        self.assertEqual(code, 0, stderr)
        return graph, stderr

    def test_references_deeper_than_the_expansion_limit_stay_literal(self):
        files = {
            "a/ya.make": module("LIBRARY", "COPY_FILE(AUTO s.cpp d.cpp OUTPUT_INCLUDES ${R1} ${B1} ${G1} ${P1})"),
            "a/s.cpp": "", "a/b.h": "", "inc/x.h": "", "plain.h": "",
        }
        args = (
            reference_chain("R", "${ARCADIA_ROOT}/inc/x.h")
            + reference_chain("B", "${BINDIR}/b.h")
            + reference_chain("G", "${ARCADIA_BUILD_ROOT}/gen/y.h")
            + reference_chain("P", "plain.h")
        )
        graph, stderr = self.make(files, "a", "-k", *args)
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/d.cpp")["inputs"],
            ["$(S)/a/s.cpp", "$(S)/plain.h", "$(S)/inc/x.h", "$(S)/a/b.h"],
        )
        self.assertEqual(
            stderr,
            'missing-include: $(B)/a/d.cpp: unresolved include "gen/y.h" — not found in source, build, search path, or sysincl',
        )

    def test_enum_headers_addressed_through_root_variables(self):
        files = {
            "a/ya.make": module("LIBRARY", (
                "GENERATE_ENUM_SERIALIZATION(${CURDIR}/e1.h)\n"
                "CONFIGURE_FILE(e2.h.in e2.h)\n"
                "GENERATE_ENUM_SERIALIZATION(${BINDIR}/e2.h)\n"
                "CONFIGURE_FILE(e3.h.in ${ARCADIA_BUILD_ROOT}/gen/e3.h)\n"
                "GENERATE_ENUM_SERIALIZATION(${ARCADIA_BUILD_ROOT}/gen/e3.h)"
            )),
            "a/e1.h": "enum A {};\n", "a/e2.h.in": "", "a/e3.h.in": "",
            "tools/enum_parser/enum_serialization_runtime/ya.make": module("LIBRARY", ""),
        }
        lib.tool_program(files, "tools/enum_parser/enum_parser", "enum_parser")
        graph, _ = self.make(files, "a", "-k")
        self.assertEqual({
            node["outputs"][0]: node["inputs"][-1]
            for node in graph["graph"] if node["kv"]["p"] == "EN"
        }, {
            "$(B)/a/e1.h_serialized.cpp": "$(S)/a/e1.h",
            "$(B)/a/e2.h_serialized.cpp": "$(B)/a/e2.h",
            "$(B)/gen/e3.h_serialized.cpp": "$(B)/gen/e3.h",
        })

    def test_archives_resources_and_matrixnet(self):
        files = {
            "a/ya.make": module("LIBRARY", (
                "ARCHIVE(NAME x.inc DONTCOMPRESS a.txt)\n"
                "ARCHIVE_BY_KEYS(NAME k.inc KEYS key DONTCOMPRESS a.txt)\n"
                "ARCHIVE_ASM(NAME asmname DONTCOMPRESS a.txt)\n"
                "RESOURCE(a/r.txt /k)\n"
                "BUILD_MN(info.mninfo MnName)\n"
                "SRCS(x.cpp)"
            )),
            "a/x.cpp": '#include "x.inc"\n#include "k.inc"\n',
            "a/a.txt": "", "a/r.txt": "", "a/info.mninfo": "",
            "library/cpp/resource/ya.make": module("LIBRARY", ""),
        }
        for tool in ("contrib/tools/yasm", "tools/archiver", "tools/rescompiler", "tools/rescompressor"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        graph, _ = self.make(files, "a", "-k", platform="default-linux-x86_64")
        self.assertEqual(lib.node_by_output(graph, "$(B)/a/x.inc")["cmds"][0]["cmd_args"][1:3], ["-q", "-x"])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/k.inc")["cmds"][0]["cmd_args"][1:7],
            ["-q", "-x", "-p", "$(S)/a/a.txt", "-k", "key"],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/asmname.rodata.o")["outputs"],
            ["$(B)/a/asmname.rodata.asm", "$(B)/a/asmname.rodata.o"],
        )
        self.assertIn("$(S)/a/r.txt", lib.node_by_output_prefix(graph, "$(B)/a/objcopy_")["inputs"])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/mn.MnName.cpp")["outputs"],
            ["$(B)/a/mn.MnName.cpp", "$(B)/a/MN_External_MnName.rodata"],
        )

    def test_dll_tool_links_its_exports_script(self):
        files = {
            "a/ya.make": module("DLL_TOOL", "EXPORTS_SCRIPT(e.exports)\nSRCS(x.cpp)").replace("DLL_TOOL()", "DLL_TOOL(dt)"),
            "a/x.cpp": "", "a/e.exports": "",
        }
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
        graph, _ = self.make(files)
        self.assertIn("$(S)/a/e.exports", lib.node_by_output(graph, "$(B)/a/libdt.so")["inputs"])

    def test_unknown_source_extension_is_reported(self):
        code, graph, stderr = ay_make({
            "a/ya.make": module("LIBRARY", "SRCS(x.xyz x.cpp)"),
            "a/x.xyz": "", "a/x.cpp": "",
        }, "a", "-k")
        self.assertEqual((code, stderr), (0, 'unsupported-source: a: unsupported source extension in "x.xyz"'))
        self.assertEqual(
            [node["outputs"][0] for node in graph["graph"] if node["kv"]["p"] == "CC"],
            ["$(B)/a/x.cpp.o"],
        )

    def test_proto_imports_of_event_and_config_protos(self):
        files = {
            "a/ya.make": module("PROTO_LIBRARY", "SRCS(x.proto)"),
            "a/x.proto": 'syntax = "proto3";\nimport "a/b.ev";\nimport "a/c.cfgproto";\n',
            "a/b.ev": "", "a/c.cfgproto": "",
            "contrib/libs/protobuf/ya.make": module("LIBRARY", ""),
        }
        for tool in ("contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        graph, stderr = self.make(files, "a", "-k")
        warnings = stderr.splitlines()
        self.assertIn('missing-include: $(B)/a/x.pb.cc: unresolved include "a/b.ev.pb.h" — not found in source, build, search path, or sysincl', warnings)
        self.assertIn('missing-include: $(B)/a/x.pb.cc: unresolved include "a/c.cfgproto.pb.h" — not found in source, build, search path, or sysincl', warnings)

    def test_run_program_input_naming_the_module_directory(self):
        files = {"a/ya.make": module("LIBRARY", "RUN_PROGRAM(tools/gen IN . OUT x.cpp)")}
        lib.tool_program(files, "tools/gen", "gen")
        graph, stderr = self.make(files, "a", "-k")
        self.assertIn("$(S)/a", lib.node_by_output(graph, "$(B)/a/x.cpp")["inputs"])
        self.assertEqual(
            stderr,
            'missing-include: $(B)/a/x.cpp: unresolved include "a" — not found in source, build, search path, or sysincl',
        )

    def test_gas_sources_and_generated_proto_sources(self):
        graph, _ = self.make({
            "a/ya.make": module("LIBRARY", "SRCS(x.S sub/y.s)"),
            "a/x.S": "", "a/sub/y.s": "",
        })
        self.assertEqual(
            sorted(node["outputs"][0] for node in graph["graph"] if node["kv"]["p"] == "AS"),
            ["$(B)/a/_/sub/y.s.o", "$(B)/a/x.S.o"],
        )

        files = {
            "a/ya.make": module("PROTO_LIBRARY", "CONFIGURE_FILE(x.proto.in x.proto)\nSRCS(${BINDIR}/x.proto)"),
            "a/x.proto.in": 'syntax = "proto3";\n',
            "contrib/libs/protobuf/ya.make": module("LIBRARY", ""),
        }
        for tool in ("contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        graph, _ = self.make(files, "a", "-k")
        protoc = lib.node_by_output(graph, "$(B)/a/x.pb.h")
        self.assertEqual(protoc["outputs"], ["$(B)/a/x.pb.h", "$(B)/a/x.pb.cc"])
        self.assertIn("$(B)/a/x.proto", protoc["inputs"])

    def test_declare_in_dirs_excludes_match_source_root_relative_paths(self):
        graph, _ = self.make({
            "a/ya.make": module("LIBRARY", "DECLARE_IN_DIRS(R *.txt DIRS d RECURSIVE EXCLUDES a/d/sub/x.txt)\nCFLAGS(-DR ${R_FILES})\nSRCS(x.cpp)"),
            "a/x.cpp": "", "a/d/a.txt": "", "a/d/sub/x.txt": "", "a/d/sub/y.txt": "",
        })
        args = lib.node_by_output(graph, "$(B)/a/x.cpp.o")["cmds"][0]["cmd_args"]
        start = args.index("-DR")
        self.assertEqual(args[start:start + 3], ["-DR", "d/a.txt", "d/sub/y.txt"])

    def test_perf_parser_scans_c_family_sources(self):
        with tempfile.TemporaryDirectory(prefix="ay-perf-test-") as directory:
            Path(directory, "a.cpp").write_text('#include "b.h"\n')
            Path(directory, "b.h").write_text("#pragma once\n")
            Path(directory, "notes.txt").write_text("not a source\n")
            result = lib.run("dev", "perf", "parser", directory, timeout=30)
        size = len('#include "b.h"\n') + len("#pragma once\n")
        self.assertRegex(result.stdout, rf"^files=2 bytes={size} iters=[1-9][0-9]* per-pass=")


if __name__ == "__main__":
    unittest.main(verbosity=2)
