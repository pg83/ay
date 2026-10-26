import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from tst import lib


RED = "\x1b[31m"
RESET = "\x1b[0m"


def compact(value):
    return json.dumps(value, separators=(",", ":"))


def raw_node(uid, kind, outputs, *, deps=(), inputs=(), args=None, **extra):
    node = {
        "uid": uid,
        "kv": {"p": kind},
        "outputs": list(outputs),
        "deps": list(deps),
        "inputs": list(inputs),
        "cmds": [{"cmd_args": list(args)}] if args is not None else [],
        "platform": "linux",
    }
    node.update(extra)
    return node


def diff_node(self_uid, outputs, *, kind="CC", args=(), uid=None, **extra):
    node = {
        "uid": uid or self_uid,
        "self_uid": self_uid,
        "kv": {"p": kind} if kind is not None else {},
        "outputs": list(outputs),
        "inputs": [],
        "deps": [],
        "cmds": [{"cmd_args": list(args)}] if args else [],
        "platform": "linux",
    }
    node.update(extra)
    return node


SUMMARY = (
    "=== outputs only in LEFT (17) ===\n"
    "  by kind:\n"
    "      13  EN\n"
    "       2  CC\n"
    "       2  PY\n"
    "  by ext:\n"
    "       1  .e00\n"
    "       1  .e01\n"
    "       1  .e02\n"
    "       1  .e03\n"
    "       1  .e04\n"
    "       1  .e05\n"
    "       1  .e06\n"
    "       1  .e07\n"
    "       1  .e08\n"
    "       1  .e09\n"
    "       1  .e10\n"
    "       1  .e11\n"
    "  by dir:\n"
    "       1  /abs/file.txt\n"
    "       1  /e/f.e00\n"
    "       1  /e/f.e01\n"
    "       1  /e/f.e02\n"
    "       1  /e/f.e03\n"
    "       1  /e/f.e04\n"
    "       1  /e/f.e05\n"
    "       1  /e/f.e06\n"
    "       1  /e/f.e07\n"
    "       1  /e/f.e08\n"
    "       1  /e/f.e09\n"
    "       1  /e/f.e10\n"
    "       1  /e/f.e11\n"
    "       1  /e/f.e12\n"
    "       1  a/b/c\n"
    "=== outputs only in RIGHT (1) ===\n"
    "  by kind:\n"
    "       1  AR\n"
    "  by ext:\n"
    "       1  .a\n"
    "  by dir:\n"
    "       1  r/lib.a\n"
)

class DumpToolTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-dump-tool-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def write(self, name, content):
        path = self.root / name
        path.write_text(content)
        return path

    def write_jsonl(self, name, nodes):
        return self.write(name, "".join(compact(node) + "\n" for node in nodes))

    def ay(self, *args, input=None, env=None):
        return subprocess.run(
            [str(lib.AY), *map(str, args)],
            cwd=self.root,
            env=env,
            input=input,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=60,
            check=False,
        )

    def norm(self, *args, input=None):
        return subprocess.run(
            [str(lib.NORM), *map(str, args)],
            cwd=self.root,
            input=input,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=60,
            check=False,
        )

    def norm_ok(self, *args, input=None):
        result = self.norm(*args, input=input)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def norm_fails(self, message, *args, input=None):
        result = self.norm(*args, input=input)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertEqual(result.stderr, "ay_norm: " + message + "\n")

    def ok(self, *args, input=None):
        result = self.ay(*args, input=input)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def fails(self, message, *args, input=None, env=None):
        result = self.ay(*args, input=input, env=env)
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertEqual(result.stderr, RED + message + RESET + "\n")


class DumpSortTest(DumpToolTest):
    def test_sort_stdin_to_stdout(self):
        result = self.norm_ok("sort", input="b\nc\na\n")
        self.assertEqual(result.stdout, "a\nb\nc\n")
        self.assertEqual(sorted(path.name for path in self.root.iterdir()), [])

    def test_sort_dash_paths_and_exact_chunk_boundary(self):
        result = self.norm_ok(
            "sort", "--in", "-", "--out", "-", "--chunk-bytes", "2",
            input="b\na\n",
        )
        self.assertEqual(result.stdout, "a\nb\n")

    def test_sort_empty_input(self):
        source = self.write("empty.txt", "")
        output = self.root / "sorted.txt"
        self.norm_ok("sort", "--in", source, "--out", output)
        self.assertEqual(output.read_text(), "")

    @unittest.skipIf(os.geteuid() == 0, "root ignores directory permissions")
    def test_sort_spills_to_cwd_when_output_dir_is_read_only(self):
        source = self.write("in.txt", "b\na\n")
        locked = self.root / "locked"
        locked.mkdir()
        output = locked / "sorted.txt"
        output.write_text("stale\n")
        locked.chmod(0o555)
        self.addCleanup(locked.chmod, 0o755)
        self.norm_ok("sort", "--in", source, "--out", output, "--chunk-bytes", "2")
        self.assertEqual(output.read_text(), "a\nb\n")
        self.assertEqual(sorted(path.name for path in self.root.iterdir()), ["in.txt", "locked"])

    def test_sort_argument_errors(self):
        self.norm_fails('sort: unknown argument "--bogus"', "sort", "--bogus")
        self.norm_fails('dump: missing value for flag "--in"', "sort", "--in")
        self.norm_fails(
            'strconv.Atoi: parsing "x": invalid syntax',
            "sort", "--chunk-bytes", "x",
        )


class DumpGrepTest(DumpToolTest):
    def test_grep_raw_graph_by_self_uid_and_later_output(self):
        source = self.write("raw.json", compact({
            "conf": {"skip": [1, 2]},
            "graph": [
                {"self_uid": "AA", "outputs": ["$(BUILD_ROOT)/x.o", "$(BUILD_ROOT)/y.o"]},
                {"self_uid": "BB", "outputs": ["$(BUILD_ROOT)/z.o"]},
                {"self_uid": "CC", "outputs": ["$(BUILD_ROOT)/w.o"]},
            ],
        }))
        result = self.ok(
            "dev", "dump", "grep", "--raw", "--in", source, "AA", "$(B)/z.o",
        )
        self.assertEqual(result.stdout, (
            '{\n  "outputs": [\n    "$(BUILD_ROOT)/x.o",\n    "$(BUILD_ROOT)/y.o"\n  ],\n'
            '  "self_uid": "AA"\n}\n'
            '{\n  "outputs": [\n    "$(BUILD_ROOT)/z.o"\n  ],\n  "self_uid": "BB"\n}\n'
        ))

    def test_grep_reads_keys_from_stdin(self):
        source = self.write_jsonl("g.jsonl", [
            {"self_uid": "AA", "outputs": ["/a"]},
            {"self_uid": "BB", "outputs": ["/b"]},
        ])
        result = self.ok("dev", "dump", "grep", "--in", source, input="\n  BB  \n\n")
        self.assertEqual(result.stdout, '{\n  "outputs": [\n    "/b"\n  ],\n  "self_uid": "BB"\n}\n')

    def test_grep_errors(self):
        source = self.write_jsonl("g.jsonl", [{"self_uid": "AA", "outputs": ["/a"]}])
        self.fails(
            "dump grep: --substr and --regex are mutually exclusive",
            "dev", "dump", "grep", "--in", source, "--substr", "--regex", "x",
        )
        self.fails("dump grep: --in is required", "dev", "dump", "grep", "x")
        self.fails('dump grep: unknown argument "--bogus"', "dev", "dump", "grep", "--bogus")
        self.fails(
            "dump grep: no keys given (positional args or stdin)",
            "dev", "dump", "grep", "--in", source,
            input="\n \n",
        )
        self.fails(
            "error parsing regexp: missing closing ): `(`",
            "dev", "dump", "grep", "--in", source, "--regex", "(",
        )


class DumpGraphErrorsTest(DumpToolTest):
    def test_malformed_raw_graphs(self):
        cases = {
            "array.json": ("[]", "expected top-level JSON object"),
            "object.json": ('{"graph": {}}', "graph is not an array"),
            "nograph.json": ('{"conf": {"a": 1}, "result": []}', 'no "graph" key found'),
            "numeric_key.json": ('{1: []}', "expected object key"),
        }
        for name, (content, message) in cases.items():
            path = self.write(name, content)
            self.norm_fails(
                f"dump: {path}: {message}",
                "normalize", "--in", path,
            )
            self.fails(
                f"dump: {path}: {message}",
                "dev", "dump", "grep", "--raw", "--in", path, "key",
            )


class DumpNormalizeTest(DumpToolTest):
    def normalize(self, nodes, *args, result):
        raw = self.write("raw.json", compact({"conf": {}, "graph": nodes, "result": list(result)}))
        normalized = self.norm_ok("normalize", "--in", raw, *args)
        return [json.loads(line) for line in normalized.stdout.splitlines()]

    def test_normalize_closure_uids_fetch_and_streaming(self):
        nodes = [
            raw_node(
                "root", "LD", ["$(BUILD_ROOT)/app/app"],
                deps=["left", "right", "fetch", "vcs", "ghost"],
                inputs=["$(B)/resources/CLANG20/bin/clang", "$(B)/resources/LLD_ROOT/ld"],
                args=["ld", "-o", "$(BUILD_ROOT)/app/app"],
                requirements={"cpu": 4, "network": "restricted", "sandbox": True},
            ),
            raw_node("left", "CC", ["$(B)/app/l.o"], deps=["leaf", "fetch"]),
            raw_node("right", "CC", ["$(B)/app/r.o"], deps=["leaf"]),
            raw_node("leaf", "PB", ["$(B)/app/gen.h"]),
            raw_node("fetch", "FT", ["$(B)/fetched"]),
            raw_node("vcs", "CP", ["$(BUILD_ROOT)/vcs.json"]),
            raw_node("outside", "CC", ["$(B)/other/o.o"]),
            {"uid": 17, "outputs": ["$(B)/numeric"], "deps": []},
        ]
        normalized = self.normalize(nodes, result=["root"])
        streamed = self.normalize(nodes, "--streaming", result=["root"])
        self.assertEqual(
            sorted(map(compact, normalized)),
            sorted(map(compact, streamed)),
        )
        by_output = {node["outputs"][0]: node for node in normalized}
        self.assertEqual(
            sorted(by_output),
            ["$(B)/app/app", "$(B)/app/gen.h", "$(B)/app/l.o", "$(B)/app/r.o"],
        )
        root = by_output["$(B)/app/app"]
        self.assertEqual(root["inputs"], ["$(CLANG)/bin/clang", "$(LLD_ROOT)/ld"])
        self.assertEqual(
            root["requirements"],
            {"cpu": 4, "network": "restricted", "sandbox": True},
        )
        self.assertEqual(root["env"], {})
        leaf_uid = by_output["$(B)/app/gen.h"]["uid"]
        self.assertEqual(by_output["$(B)/app/l.o"]["deps"], [leaf_uid])
        self.assertEqual(by_output["$(B)/app/r.o"]["deps"], [leaf_uid])
        self.assertEqual(
            root["deps"],
            sorted([
                by_output["$(B)/app/l.o"]["uid"],
                by_output["$(B)/app/r.o"]["uid"],
                "ghost",
            ]),
        )
        self.assertEqual(len({node["uid"] for node in normalized}), 4)

    def test_normalize_uses_result_even_for_nonstandard_roots(self):
        archive = raw_node("ar", "AR", ["$(B)/lib/liblib.a"], deps=["cc"])
        compile_node = raw_node("cc", "CC", ["$(B)/lib/a.o"])
        test_node = raw_node("ts", "TS", ["$(B)/lib/test.out"], deps=["cc"], kv={"p": "TS", "path": "lib/ut"})
        other_test = raw_node("ts2", "TS", ["$(B)/x/test.out"], kv={"p": "TS", "path": "x/ut"})
        normalized = self.normalize([archive, compile_node, test_node, other_test], result=["ts"])
        self.assertEqual(
            sorted(node["outputs"][0] for node in normalized),
            ["$(B)/lib/a.o", "$(B)/lib/test.out"],
        )
        normalized = self.normalize([archive, compile_node], result=["ar"])
        self.assertEqual(
            sorted(node["outputs"][0] for node in normalized),
            ["$(B)/lib/a.o", "$(B)/lib/liblib.a"],
        )
        first = raw_node("ld1", "LD", ["$(B)/app/one"])
        second = raw_node("pkg", "UN", ["$(B)/elsewhere/package.pkg.fake"])
        normalized = self.normalize([first, second], result=["pkg"])
        self.assertEqual(
            sorted(node["outputs"][0] for node in normalized),
            ["$(B)/elsewhere/package.pkg.fake"],
        )

    def test_normalize_multiple_results_and_streaming(self):
        nodes = [
            raw_node("ld", "LD", ["$(B)/app/app"], deps=["cc"]),
            raw_node("cc", "CC", ["$(B)/app/main.o"]),
            raw_node("ar", "AR", ["$(B)/lib/liblib.a"], deps=["lcc"]),
            raw_node("lcc", "CC", ["$(B)/lib/a.o"]),
            raw_node("ts", "TS", ["$(B)/lib/ut/test.out"], deps=["ar"], kv={"p": "TS", "path": "lib/ut"}),
            raw_node("outside", "CC", ["$(B)/other/o.o"]),
        ]
        both = self.normalize(nodes, result=["ld", "ts"])
        self.assertEqual(
            sorted(node["outputs"][0] for node in both),
            ["$(B)/app/app", "$(B)/app/main.o", "$(B)/lib/a.o", "$(B)/lib/liblib.a", "$(B)/lib/ut/test.out"],
        )
        singles = self.normalize(nodes, result=["ld"]) + self.normalize(nodes, result=["ts"])
        self.assertEqual(sorted(map(compact, both)), sorted(set(map(compact, singles))))
        self.assertEqual(
            sorted(map(compact, both)),
            sorted(map(compact, self.normalize(nodes, result=["ld", "ts", "ar"]))),
        )
        self.assertEqual(
            sorted(map(compact, both)),
            sorted(map(compact, self.normalize(nodes, "--streaming", result=["ld", "ts"]))),
        )

    def test_normalize_includes_all_results_regardless_of_path_or_kind(self):
        nodes = [
            raw_node(1, "LD", ["$(B)/app/app"]),
            raw_node(2, "PK", ["$(B)/pkg/pkg.tar"], deps=[3]),
            raw_node(3, "CP", ["$(B)/pkg/data/file"], deps=[1]),
            raw_node(4, "CP", ["$(B)/pkg/stray"]),
            raw_node(5, "JV", ["$(B)/java/lib.jar"]),
            raw_node(6, "CP", ["$(B)/app/extra"]),
        ]
        result = [1, 2, 5, 6]
        for streaming in ((), ("--streaming",)):
            normalized = self.normalize(nodes, *streaming, result=result)
            self.assertEqual(
                sorted(node["outputs"][0] for node in normalized),
                ["$(B)/app/app", "$(B)/app/extra", "$(B)/java/lib.jar", "$(B)/pkg/data/file", "$(B)/pkg/pkg.tar"],
            )
        normalized = self.normalize(nodes, result=[5])
        self.assertEqual([node["outputs"][0] for node in normalized], ["$(B)/java/lib.jar"])

    def test_normalize_follows_result_not_output_prefix(self):
        raw = self.write("roots.json", compact({"graph": [
            raw_node("ld", "LD", ["$(B)/app/app"]),
            raw_node("cp", "CP", ["$(B)/pkg/file"]),
        ], "result": ["ld", "cp"]}))
        for flags in ((), ("--streaming",), ("--ref-graph",)):
            result = self.norm_ok("normalize", "--in", raw, *flags)
            self.assertEqual(result.stderr, "")
            self.assertEqual(
                sorted(json.loads(line)["outputs"][0] for line in result.stdout.splitlines()),
                ["$(B)/app/app", "$(B)/pkg/file"],
            )

    def test_normalize_ref_graph_preserves_ordering_deps(self):
        nodes = [
            raw_node(
                "root", "LD", ["$(B)/app/app"],
                deps=["in_inputs", "in_cmd", "ordering", "no_outputs", "fetch"],
                inputs=["$(B)/app/a.o"],
                args=["ld", "--script=$(B)/app/script.ld"],
            ),
            raw_node("in_inputs", "CC", ["", "$(B)/app/a.o"]),
            raw_node("in_cmd", "PY", ["", "$(B)/app/script.ld"]),
            raw_node("ordering", "CC", ["$(B)/app/ordering.o"]),
            raw_node("no_outputs", "CC", []),
            raw_node("fetch", "FT", ["$(B)/fetched"]),
        ]
        normalized = self.normalize(nodes, "--ref-graph", result=["root"])
        by_kind = {}
        for node in normalized:
            by_kind.setdefault(node["kv"]["p"], []).append(node)
        root = by_kind["LD"][0]
        kept = sorted(
            node["uid"] for node in normalized
            if node["uid"] != root["uid"]
        )
        self.assertEqual(root["deps"], kept)
        self.assertEqual(len(normalized), 5)
        self.assertIn("$(B)/app/ordering.o", [o for n in normalized for o in n["outputs"]])
        self.assertEqual(
            sorted(map(compact, normalized)),
            sorted(map(compact, self.normalize(nodes, result=["root"]))),
        )

    def test_normalize_ref_graph_preserves_test_build_dependencies(self):
        nodes = [
            raw_node(
                "suite", "TS", ["$(B)/app/test/meta.json"],
                deps=["tool"], inputs=["$(S)/app/test"],
                args=["test_tool", "--depends", "tools/formatter"],
                kv={"p": "TS", "path": "app/test"},
            ),
            raw_node("tool", "LD", ["$(B)/tools/formatter", "$(B)/tools/formatter.license"]),
        ]
        expected = sorted(map(compact, self.normalize(nodes, result=["suite"])))
        for flags in ((), ("--streaming",)):
            normalized = self.normalize(nodes, "--ref-graph", *flags, result=["suite"])
            self.assertEqual(sorted(map(compact, normalized)), expected)
            self.assertEqual(len(normalized), 2)
            by_output = {node["outputs"][0]: node for node in normalized}
            self.assertEqual(
                by_output["$(B)/app/test/meta.json"]["deps"],
                [by_output["$(B)/tools/formatter"]["uid"]],
            )

    def test_normalize_enum_preserves_declared_inputs_producers_and_order_edges(self):
        tool = "$(B)/tools/enum_parser/enum_parser/enum_parser"
        header = "$(B)/app/types.h"
        removed = "$(B)/app/indirect.pb.h"
        extra_tool_header = "$(B)/tools/enum_parser/enum_parser/private.h"
        nodes = [
            raw_node(
                "enum", "EN", ["$(B)/app/types.h_serialized.cpp"],
                deps=["tool", "header", "indirect", "order", "extra"],
                inputs=[tool, header, removed, extra_tool_header, "$(S)/app/transitive.h"],
                args=[tool, header, "--include-path", "app/types.h", "--output", "$(B)/app/types.h_serialized.cpp"],
            ),
            raw_node("tool", "LD", [tool, extra_tool_header]),
            raw_node("header", "CP", [header]),
            raw_node("indirect", "PB", [removed, "$(B)/app/indirect.pb.cc"]),
            raw_node("order", "PR", ["$(B)/app/ordering.stamp"]),
            raw_node("extra", "PB", ["$(B)/app/not_a_raw_input.h"]),
        ]
        expected = sorted(map(compact, self.normalize(nodes, result=["enum"])))
        for flags in ((), ("--streaming",)):
            for reference in ((), ("--ref-graph",)):
                normalized = self.normalize(nodes, *reference, *flags, result=["enum"])
                self.assertEqual(sorted(map(compact, normalized)), expected)
                by_output = {node["outputs"][0]: node for node in normalized}
                generated = by_output["$(B)/app/types.h_serialized.cpp"]
                self.assertEqual(generated["inputs"], sorted(nodes[0]["inputs"]))
                self.assertEqual(generated["deps"], sorted(
                    node["uid"] for node in normalized if node is not generated))
                self.assertIn(removed, by_output)

    def test_enum_keeps_the_direct_input_producers_primary_output(self):
        tool = "$(B)/tools/enum_parser/enum_parser/enum_parser"
        header = "$(B)/app/generated.h"
        for primary in ("$(B)/app/base_gen.cpp", "$(B)/app/opaque.primary"):
            for induced in (False, True):
                with self.subTest(primary=primary, induced=induced):
                    raw_inputs = [tool, header, primary]
                    if induced:
                        raw_inputs += ["$(S)/inc/ind.h", "$(B)/app/unrelated.primary", "$(B)/app/unrelated.h"]
                    nodes = [
                        raw_node(
                            "enum", "EN", ["$(B)/app/generated.h_serialized.cpp"],
                            deps=["producer", "tool", "ordering", *(["unrelated"] if induced else [])],
                            inputs=raw_inputs,
                            args=[tool, header, "--include-path", "app/generated.h", "--output", "$(B)/app/generated.h_serialized.cpp"],
                        ),
                        raw_node("producer", "PR", [primary, header]),
                        raw_node("tool", "LD", [tool]),
                        raw_node("ordering", "PR", ["$(B)/app/order.stamp"]),
                        raw_node("unrelated", "PR", ["$(B)/app/unrelated.primary", "$(B)/app/unrelated.h"]),
                    ]
                    expected = sorted(map(compact, self.normalize(nodes, result=["enum"])))
                    for flags in ((), ("--streaming",)):
                        for reference in ((), ("--ref-graph",)):
                            normalized = self.normalize(nodes, *reference, *flags, result=["enum"])
                            self.assertEqual(sorted(map(compact, normalized)), expected)
                            generated = next(node for node in normalized if node["kv"]["p"] == "EN")
                            self.assertEqual(generated["inputs"], sorted(raw_inputs))

    def test_normalize_preserves_declared_action_inputs(self):
        root = raw_node(
            "root", "LD", ["$(B)/app/app"],
            deps=["objcopy", "raw", "cy", "ar", "pr"],
            inputs=[
                "$(S)/lib/prebuilt.a",
                "$(B)/lib/liblib.a",
                "$(S)/build/scripts/link_exe.py",
                "$(S)/build/scripts/other.py",
                "$(S)/contrib/gnu",
                "$(S)/tools/named.txt",
            ],
            args=[
                "ld", "gnu", "named.txt", "$(B)/app/objcopy.o", "$(B)/app/x_raw.auxcpp",
                "$(B)/app/m.pyx.cpp", "$(B)/app/plain.cpp",
            ],
        )
        objcopy = raw_node(
            "objcopy", "PY", ["$(B)/app/objcopy.o"],
            inputs=[
                "$(S)/build/scripts/objcopy.py",
                "$(S)/contrib/libs/x/data.bin",
                "$(S)/app/payload.h",
                "$(S)/build/scripts/helper.py",
                "$(S)/app/resource.bin",
                "$(B)/app/generated.bin",
                "$(S)/app/gnu",
                "$(S)/app/extra.cpp",
            ],
            args=["python3", "$(S)/build/scripts/objcopy.py", "gnu", "$(S)/app/payload.h"],
        )
        raw = raw_node(
            "raw", "PR", ["$(B)/app/x_raw.auxcpp"],
            inputs=["$(B)/tools/rescompiler", "$(S)/app/data.txt", "$(S)/app/other.txt"],
            cmds=[
                {"cmd_args": []},
                {"cmd_args": ["$(B)/tools/rescompiler", "out", "$(S)/app/data.txt", "key", "-"]},
            ],
        )
        cython = raw_node(
            "cy", "CY", ["$(B)/app/m.pyx.cpp"],
            inputs=["$(B)/app/generated.pxi", "$(S)/app/m.pyx", "$(S)/app/m.h"],
            args=["cython", "$(S)/app/m.pyx"],
        )
        archive = raw_node(
            "ar", "AR", ["$(B)/lib/liblib.a"],
            inputs=["$(S)/build/scripts/link_lib.py", "$(B)/lib/a.o"],
            args=["ar", "$(B)/lib/a.o"],
        )
        plain_pr = raw_node(
            "pr", "PR", ["$(B)/app/plain.cpp"],
            inputs=["$(S)/app/plain.in", "$(S)/app/extra.h"],
            args=["gen", "$(S)/app/plain.in"],
        )
        normalized = self.normalize(
            [root, objcopy, raw, cython, archive, plain_pr], "--ref-graph", result=["root"],
        )
        inputs = {node["outputs"][0]: node["inputs"] for node in normalized}
        self.assertEqual(inputs["$(B)/app/app"], sorted(root["inputs"]))
        self.assertEqual(inputs["$(B)/app/objcopy.o"], sorted(objcopy["inputs"]))
        self.assertEqual(inputs["$(B)/app/x_raw.auxcpp"], sorted(raw["inputs"]))
        self.assertEqual(inputs["$(B)/app/m.pyx.cpp"], sorted(cython["inputs"]))
        self.assertEqual(inputs["$(B)/lib/liblib.a"], [
            "$(B)/lib/a.o",
            "$(S)/build/scripts/link_lib.py",
        ])
        self.assertEqual(inputs["$(B)/app/plain.cpp"], [
            "$(S)/app/extra.h",
            "$(S)/app/plain.in",
        ])

    def test_normalize_errors(self):
        self.norm_fails("normalize: --in is required", "normalize")
        self.norm_fails('normalize: unknown argument "--bogus"', "normalize", "--bogus")
        self.norm_fails('normalize: unknown argument "--target"', "normalize", "--target")
        raw = self.write("no_roots.json", compact({"graph": [], "result": []}))
        self.norm_fails("normalize: graph has no result roots", "normalize", "--in", raw)
        raw = self.write("missing_root.json", compact({"graph": [], "result": ["missing"]}))
        self.norm_fails(
            'normalize: result root "missing" is absent from graph',
            "normalize", "--in", raw,
        )


class DumpDiffTest(DumpToolTest):
    def diff(self, left, right, *mode):
        left_path = self.write_jsonl("left.jsonl", left)
        right_path = self.write_jsonl("right.jsonl", right)
        return self.ok("dev", "dump", "diff", "--left", left_path, "--right", right_path, *mode).stdout

    def test_summary_groups_one_sided_outputs(self):
        left = [
            diff_node("A", ["$(B)/a/b/c/d.pic.o", "$(B)/a/x.pb.cc"], kind="CC"),
            diff_node("B", ["$(S)/src/tool", "/abs/file.txt"], kind="PY"),
            diff_node("E", [f"/e/f.e{i:02d}" for i in range(13)], kind="EN"),
            diff_node("S", ["/shared"]),
        ]
        right = [
            diff_node("S", ["/shared"]),
            diff_node("C", ["$(B)/r/lib.a"], kind="AR"),
        ]
        self.assertEqual(self.diff(left, right, "--summary"), SUMMARY)

    def test_sections_report_mismatched_uid_sets(self):
        left = [diff_node("A", ["/x"]), diff_node("B", ["/x"])]
        right = [diff_node("A", ["/x"])]
        output = self.diff(left, right)
        self.assertIn(
            "=== outputs in both with mismatched self_uid (1) ===\n/x  left=[A,B] right=[A]\n",
            output,
        )

    def test_by_token_categories_and_ranking(self):
        tokens = [
            "-Iinc", "-DX", "-Llib", "-lz", "-Wall", "-march=x", "-fpic",
            "${UNSET}", "a${B}", "-x", "$(B)/p", "dir/p", "word",
        ] + [f"extra{i:02d}" for i in range(20)]
        left = [diff_node("L", ["/o"], args=["cc", *tokens])]
        right = [diff_node("R", ["/o"], args=["cc", "only-ref"])]
        output = self.diff(left, right, "--by-token")
        self.assertIn("=== by-token: 1 outputs in both ===\n", output)
        self.assertIn(
            "[cmds tokens only in OURS]  (token: #nodes, by category)\n"
            "  totals:\n"
            "      21  other\n"
            "       2  UNEXPANDED\n"
            "       2  path\n"
            "       1  def\n"
            "       1  fflag\n"
            "       1  flag\n"
            "       1  incl\n"
            "       1  lib\n"
            "       1  libdir\n"
            "       1  march\n"
            "       1  warn\n"
            "       1  [path] $(B)/p\n"
            "       1  [UNEXPANDED] ${UNSET}\n"
            "       1  [def] -DX\n",
            output,
        )
        ranking = output.split("[cmds tokens only in OURS]")[1].split("[cmds tokens only in REF]")[0]
        ranked = [line for line in ranking.splitlines() if line.startswith("       1  [")]
        self.assertEqual(len(ranked), 25)
        self.assertEqual(ranked[-1], "       1  [other] extra12")
        self.assertIn(
            "[cmds tokens only in REF]  (token: #nodes, by category)\n"
            "  totals:\n"
            "       1  other\n"
            "       1  [other] only-ref\n",
            output,
        )

    def test_by_token_ranks_by_node_count(self):
        left = [
            diff_node("L1", ["/a"], args=["cc", "twice", "once-a"]),
            diff_node("L2", ["/b"], args=["cc", "twice"]),
            diff_node("dup", ["/c"], args=["cc"]),
            diff_node("dup", ["/c"], args=["cc"]),
        ]
        right = [
            diff_node("R1", ["/a"], args=["cc"]),
            diff_node("R2", ["/b"], args=["cc"]),
            diff_node("dup", ["/c"], args=["cc"]),
        ]
        output = self.diff(left, right, "--by-token")
        self.assertIn("=== by-token: 3 outputs in both ===\n", output)
        self.assertIn(
            "  totals:\n"
            "       3  other\n"
            "       2  [other] twice\n"
            "       1  [other] once-a\n",
            output,
        )

    def test_pic_outputs_pair_with_pic_variants(self):
        left = [diff_node("L-pic", ["/x.pic.o"], args=["cc", "ours-pic"])]
        right = [diff_node("R-pic", ["/x.pic.o"], args=["cc", "ref-pic"])]
        output = self.diff(left, right, "--by-token")
        self.assertIn("=== by-token: 1 outputs in both ===\n", output)
        self.assertIn("       1  [other] ours-pic\n", output)
        self.assertIn("       1  [other] ref-pic\n", output)

    def test_by_token_groups_and_unmatched_nodes(self):
        left = [
            diff_node("L1", ["$(B)/d/a.o", "$(B)/c/a.o"], kind=None, args=["cc", "ours"]),
            diff_node("L2", [], kind="CC", args=["cc", "lonely"]),
            diff_node("L3", ["/left-only"], args=["cc"]),
        ]
        right = [
            diff_node("R1", ["$(B)/d/a.o"], kind=None, args=["cc", "ref"]),
        ]
        output = self.diff(left, right, "--by-token", "--group", " dir , kind ")
        self.assertIn("########## group: dir=c/a.o kind=(none) ##########", output)
        self.assertNotIn("lonely", output)

    def test_by_token_roots_skips_canceled_non_roots(self):
        left = [
            diff_node("same", ["/same"], uid="Ls", args=["cc", "same"]),
            diff_node("LD", ["/div"], uid="Ld", args=["cc", "ours"]),
        ]
        right = [
            diff_node("same", ["/same"], uid="Rs", args=["cc", "same"]),
            diff_node("RD", ["/div"], uid="Rd", args=["cc", "ref"]),
        ]
        output = self.diff(left, right, "--by-token", "--roots")
        self.assertIn("=== by-token: 1 outputs in both (roots only) ===\n", output)

    def test_match_fallbacks_across_host_and_platform(self):
        left = [
            diff_node("L-host", ["/host"], args=["cc", "left-host"], host_platform=True),
            diff_node("L-plat", ["/plat"], args=["cc", "left-plat"], platform="linux-x86_64"),
            diff_node("L-pic", ["/pic.a"], args=["ar", "x.pic.o"]),
            diff_node("L-missing", ["/missing"], args=["cc"]),
        ]
        right = [
            diff_node("R-host", ["/host"], args=["cc", "right-host"]),
            diff_node("R-plat", ["/plat"], args=["cc", "right-plat"], platform="darwin"),
            diff_node("R-pic", ["/pic.a"], args=["ar", "y.pic.o"]),
        ]
        by_field = self.diff(left, right, "--by-field")
        self.assertIn("=== by-field: 3 outputs in both ===\n", by_field)
        self.assertIn("       3 (100.0%)  cmds\n", by_field)
        self.assertIn("       1 ( 33.3%)  platform\n", by_field)
        self.assertIn("       2  cmds\n       1  cmds+platform\n", by_field)
        by_token = self.diff(left, right, "--by-token")
        for token in ("left-host", "right-host", "left-plat", "right-plat", "x.pic.o", "y.pic.o"):
            self.assertIn(token, by_token)
        by_kind = self.diff(left, right, "--by-kind")
        self.assertEqual(by_kind, (
            "=== by-kind: content divergence per node kind ===\n"
            "kind       paired  diverge   top differing fields / combos\n"
            "CC              3        3   platform:1 cmds:3   [top combo: cmds ×2]\n"
        ))
        roots = self.diff(left, right, "--roots")
        self.assertIn("=== roots: 3 leaf-most divergent outputs (of 3 divergent) ===\n", roots)

    def test_by_kind_counts_kindless_and_unmatched(self):
        left = [
            diff_node("L1", ["/a"], kind=None, args=["x"]),
            diff_node("L2", ["/b"], kind="CC", args=["y"]),
            diff_node("L3", ["/c"], kind="CC", args=["z"]),
            diff_node("L4", ["/lonely"], kind="AR"),
        ]
        right = [
            diff_node("R1", ["/a"], kind=None, args=["x2"]),
            diff_node("R2", ["/b"], kind="CC", args=["y"]),
            diff_node("R3", ["/c"], kind="CC", args=["z2"]),
            diff_node("R2dup", ["/b"], kind="CC", args=["y"]),
        ]
        output = self.diff(left, right, "--by-kind")
        lines = output.splitlines()
        self.assertEqual(lines[0], "=== by-kind: content divergence per node kind ===")
        self.assertEqual(lines[2].split()[:3], ["CC", "2", "1"])
        self.assertEqual(lines[3].split()[:3], ["(none)", "1", "1"])
        self.assertNotIn("AR", output)

    def test_pair_structural_command_differences(self):
        left = [diff_node("L", ["/s"], cmds=[
            {"cmd_args": ["cc", "a"], "stdout": "/out1", "env": {"A": "1"}},
            {"cmd_args": ["cc", "b"]},
        ], env={"X": "1"})]
        right = [diff_node("R", ["/s"], cmds=[
            {"cmd_args": ["cc", "a"], "stdout": "/out2", "env": {"A": "2"}},
        ], env={"X": "2"})]
        output = self.diff(left, right, "--pair", "/s")
        self.assertIn("  -ours +b\n", output)
        self.assertIn("  -ours +cc\n", output)
        self.assertIn('[field env differs]\n  ours: {"X":"1"}\n  ref:  {"X":"2"}\n', output)
        left = [diff_node("L", ["/s"], cmds=[
            {"cmd_args": ["cc"], "stdout": "/out1", "env": {"A": "1"}},
            {"cmd_args": ["cc"]},
        ])]
        right = [diff_node("R", ["/s"], cmds=[
            {"cmd_args": ["cc", "cc"], "stdout": "/out2", "env": {"A": "2"}},
        ])]
        output = self.diff(left, right, "--pair", "/s")
        self.assertIn("  cmd count: ours=2 ref=1\n", output)
        self.assertIn("  cmd[0] stdout: ours=/out1 ref=/out2\n", output)
        self.assertIn('  cmd[0] env: ours={"A":"1"} ref={"A":"2"}\n', output)

    def test_pair_selection_fallbacks(self):
        same = {"args": ["cc", "same"]}
        cases = [
            (
                [diff_node("L", ["/p"], args=["cc", "l"], host_platform=True)],
                [diff_node("R", ["/p"], args=["cc", "r"])],
                ["+l", "+r"],
            ),
            (
                [diff_node("L1", ["/p"], **same), diff_node("L2", ["/p"], kind="PY", args=["py"])],
                [diff_node("R1", ["/p"], **same)],
                [],
            ),
            (
                [
                    diff_node("L1", ["/p"], host_platform=True, **same),
                    diff_node("L2", ["/p"], kind="PY", args=["py"]),
                ],
                [diff_node("R1", ["/p"], **same)],
                [],
            ),
            (
                [diff_node("L", ["/p"], kind="PY", args=["py"])],
                [diff_node("R", ["/p"], kind="CC", args=["cc"])],
                ["+py", "+cc"],
            ),
        ]
        for left, right, expected in cases:
            output = self.diff(left, right, "--pair", "/p")
            self.assertTrue(output.startswith("=== pair diff for /p ===\n"), output)
            for token in expected:
                self.assertIn(token, output)
            if not expected:
                self.assertNotIn("differs", output)

    def test_pair_missing_output(self):
        left_path = self.write_jsonl("left.jsonl", [diff_node("L", ["/only-left"])])
        right_path = self.write_jsonl("right.jsonl", [diff_node("R", ["/only-right"])])
        self.fails(
            'dump diff --pair: output "/only-right" not found in left',
            "dev", "dump", "diff", "--left", left_path, "--right", right_path,
            "--pair", "/only-right",
        )
        self.fails(
            'dump diff --pair: output "/only-left" not found in right',
            "dev", "dump", "diff", "--left", left_path, "--right", right_path,
            "--pair", "/only-left",
        )

    def test_diff_argument_errors(self):
        base = ("dev", "dump", "diff", "--left", "l", "--right", "r")
        self.fails(
            "dump diff: modes --by-field and --by-kind are mutually exclusive",
            *base, "--by-field", "--by-kind",
        )
        self.fails('dump diff: unknown argument "--bogus"', *base, "--bogus")
        self.fails(
            "dump diff: --left and --right are required",
            "dev", "dump", "diff", "--left", "l",
        )
        self.fails("dump diff: --roots cannot combine with --summary", *base, "--summary", "--roots")
        self.fails("dump diff: --group is only valid with --by-token", *base, "--group", "kind")
        self.fails(
            'dump diff: --group dimension "size" must be one of kind,dir',
            *base, "--by-token", "--group", "kind,size",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
