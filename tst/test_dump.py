import json
import tempfile
import unittest
from pathlib import Path

import lib


def compact(value):
    return json.dumps(value, separators=(",", ":"))


def raw_node(
    uid,
    kind,
    outputs,
    *,
    deps=(),
    inputs=(),
    cmds=(),
    tags=(),
    platform="linux",
    **extra,
):
    if isinstance(outputs, str):
        outputs = [outputs]
    return {
        "uid": uid,
        "kv": {"p": kind},
        "outputs": list(outputs),
        "deps": list(deps),
        "inputs": list(inputs),
        "cmds": list(cmds),
        "tags": list(tags),
        "requirements": {},
        "target_properties": {},
        "platform": platform,
        "env": {},
        **extra,
    }


def diff_node(
    self_uid,
    output,
    *,
    uid=None,
    kind="R6",
    deps=(),
    inputs=(),
    args=(),
    cmds=None,
    tags=(),
    host=False,
    outputs=None,
):
    if cmds is None:
        cmds = [{"cmd_args": list(args)}] if args else []
    node = raw_node(
        uid or self_uid,
        kind,
        outputs if outputs is not None else output,
        deps=deps,
        inputs=inputs,
        cmds=cmds,
        tags=tags,
    )
    node["self_uid"] = self_uid
    if host:
        node["host_platform"] = True
    return node


class DumpTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-dump-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def write(self, name, content):
        path = self.root / name
        path.write_text(content)
        return path

    def write_jsonl(self, name, nodes):
        return self.write(name, "".join(compact(node) + "\n" for node in nodes))

    def diff(self, left, right, *mode):
        left_path = self.write_jsonl("left.jsonl", left)
        right_path = self.write_jsonl("right.jsonl", right)
        output = self.root / "diff.txt"
        lib.run(
            "dev", "dump", "diff",
            "--left", left_path,
            "--right", right_path,
            "--out", output,
            *mode,
        )
        return output.read_text()

    def normalize(self, nodes, target, *, reference=False, sort=False):
        raw = self.write("raw.json", compact({"conf": {}, "graph": nodes, "result": []}))
        normalized = self.root / "normalized.jsonl"
        args = [
            "dev", "dump", "normalize",
            "--in", raw,
            "--target", target,
            "--out", normalized,
        ]
        if reference:
            args.append("--ref-graph")
        lib.run(*args)
        if not sort:
            return normalized.read_text()
        sorted_path = self.root / "sorted.jsonl"
        lib.run(
            "dev", "dump", "sort",
            "--in", normalized,
            "--out", sorted_path,
        )
        return sorted_path.read_text()

    def normalized_special_node(self, node):
        if node["kv"]["p"] == "AR":
            text = self.normalize([node], "pkg", reference=True)
        else:
            root = raw_node(
                "root",
                "LD",
                "$(B)/pkg/app/app",
                deps=[node["uid"]],
                inputs=node["outputs"],
                cmds=[{"cmd_args": ["ld", *node["outputs"]]}],
            )
            text = self.normalize([root, node], "pkg/app", reference=True)
        nodes = [json.loads(line) for line in text.splitlines()]
        return next(item for item in nodes if item["kv"].get("p") == node["kv"]["p"])

    def test_norm_path(self):
        root = raw_node(
            "root",
            "LD",
            "$(BUILD_ROOT)/pkg/app/app",
            inputs=[
                "$(BUILD_ROOT)/a/b.o",
                "$(SOURCE_ROOT)/a/b.c",
                "$(CLANG-243881345)/bin/clang",
                "$(LLD_ROOT-12)/x $(YMAKE_PYTHON3-9)/p",
                "/usr/bin/clang",
            ],
        )
        node = json.loads(self.normalize([root], "pkg/app"))
        self.assertEqual(
            node["inputs"],
            [
                "$(B)/a/b.o",
                "$(CLANG)/bin/clang",
                "$(LLD_ROOT)/x $(YMAKE_PYTHON3)/p",
                "$(S)/a/b.c",
                "/usr/bin/clang",
            ],
        )

    def test_sort_merges_chunks(self):
        source = self.write("in.txt", "cherry\napple\nbanana\napple\ndate\n")
        output = self.root / "out.txt"
        lib.run(
            "dev", "dump", "sort",
            "--in", source,
            "--out", output,
            "--chunk-bytes", "8",
        )
        self.assertEqual(output.read_text(), "apple\napple\nbanana\ncherry\ndate\n")

    def test_normalize_semantic_equivalence(self):
        target = "pkg/app"
        a = [
            raw_node(
                "u_ld", "LD", "$(B)/pkg/app/app",
                deps=["u_cc"], inputs=["$(B)/pkg/app/main.o"],
                env={"X": "$(CLANG-123)/lib"},
            ),
            raw_node(
                "u_cc", "CC", "$(B)/pkg/app/main.o",
                inputs=["$(S)/pkg/app/main.c"],
            ),
        ]
        b = [
            raw_node(
                "a2", "CC", "$(BUILD_ROOT)/pkg/app/main.o",
                inputs=["$(SOURCE_ROOT)/pkg/app/main.c"],
                stats_uid="deadbeef",
            ),
            raw_node(
                "a1", "LD", "$(BUILD_ROOT)/pkg/app/app",
                deps=["a2"], inputs=["$(BUILD_ROOT)/pkg/app/main.o"],
                env={"X": "$(CLANG-999)/lib"},
            ),
        ]
        c = [
            raw_node(
                "c_ld", "LD", "$(B)/pkg/app/app",
                deps=["c_cc"], inputs=["$(B)/pkg/app/main.o"],
                env={"X": "$(CLANG-123)/lib"},
            ),
            raw_node(
                "c_cc", "CC", "$(B)/pkg/app/main.o",
                inputs=["$(S)/pkg/app/other.c"],
            ),
        ]
        normalized_a = self.normalize(a, target, sort=True)
        normalized_b = self.normalize(b, target, sort=True)
        normalized_c = self.normalize(c, target, sort=True)
        self.assertEqual(normalized_a, normalized_b)
        self.assertNotEqual(normalized_a, normalized_c)
        self.assertEqual(normalized_a.count("\n"), 2)

    def test_diff_sections(self):
        output = self.diff(
            [
                {"self_uid": "A", "outputs": ["/x"]},
                {"self_uid": "B", "outputs": ["/y"]},
                {"self_uid": "C", "outputs": ["/shared"]},
            ],
            [
                {"self_uid": "A", "outputs": ["/x"]},
                {"self_uid": "D", "outputs": ["/z"]},
                {"self_uid": "E", "outputs": ["/shared"]},
            ],
        )
        for expected in (
            "=== self_uid only in LEFT (2) ===\nB\nC\n",
            "=== self_uid only in RIGHT (2) ===\nD\nE\n",
            "=== outputs only in LEFT (1) ===\n/y\n",
            "=== outputs only in RIGHT (1) ===\n/z\n",
            "=== outputs in both with mismatched self_uid (1) ===\n"
            "/shared  left=[C] right=[E]\n",
        ):
            self.assertIn(expected, output)

    def test_grep(self):
        source = self.write_jsonl("grep.jsonl", [
            {"self_uid": "AA", "outputs": ["$(B)/p/a.o"], "kv": {"p": "CC"}},
            {"self_uid": "BB", "outputs": ["$(B)/p/b.o"], "kv": {"p": "CC"}},
        ])
        by_output = lib.run(
            "dev", "dump", "grep", "--in", source, "$(BUILD_ROOT)/p/a.o",
        ).stdout
        self.assertIn('"AA"', by_output)
        self.assertNotIn('"BB"', by_output)
        by_uid = lib.run("dev", "dump", "grep", "--in", source, "BB").stdout
        self.assertIn('"BB"', by_uid)
        self.assertNotIn('"AA"', by_uid)

    def test_grep_substr_and_regex(self):
        source = self.write_jsonl("grep.jsonl", [
            {
                "self_uid": "AA",
                "outputs": ["/a.o"],
                "cmds": [{"cmd_args": ["clang", "${SSE41_CFLAGS}"]}],
            },
            {
                "self_uid": "BB",
                "outputs": ["/b.o"],
                "cmds": [{"cmd_args": ["clang", "-O2"]}],
            },
        ])
        substring = lib.run(
            "dev", "dump", "grep",
            "--in", source,
            "--substr", "${SSE41_CFLAGS}",
        ).stdout
        self.assertIn('"AA"', substring)
        self.assertNotIn('"BB"', substring)
        regex = lib.run(
            "dev", "dump", "grep",
            "--in", source,
            "--regex", "SSE[0-9]+_CFLAGS",
        ).stdout
        self.assertIn('"AA"', regex)
        self.assertNotIn('"BB"', regex)

    def test_diff_modes(self):
        left = [diff_node(
            "L1", "/a.o", kind="CC", inputs=["/a.c"],
            args=["clang", "-c", "${SSE}"], tags=["x"],
        )]
        right = [diff_node(
            "R1", "/a.o", kind="CC", inputs=["/a.c"],
            args=["clang", "-c", "-fno-omit-frame-pointer"],
        )]
        self.assertIn("cmds", self.diff(left, right, "--by-field"))
        by_token = self.diff(left, right, "--by-token")
        self.assertIn("${SSE}", by_token)
        self.assertIn("-fno-omit-frame-pointer", by_token)
        pair = self.diff(left, right, "--pair", "/a.o")
        self.assertIn("+${SSE}", pair)
        self.assertIn("+-fno-omit-frame-pointer", pair)

    def test_diff_modes_pair_duplicate_outputs_by_variant(self):
        left = [
            diff_node("L-host", "/dup", host=True, tags=["tool"]),
            diff_node("L-target", "/dup"),
        ]
        right = [
            diff_node("R-host", "/dup", host=True, tags=["tool"]),
            diff_node("R-target", "/dup"),
        ]
        by_field = self.diff(left, right, "--by-field")
        self.assertNotIn("host_platform", by_field)
        self.assertNotIn("tags", by_field)
        by_kind = self.diff(left, right, "--by-kind")
        self.assertNotIn("host_platform:", by_kind)
        self.assertIn("R6", by_kind)

    def test_diff_pair_prefers_divergent_duplicate_variant(self):
        left = [
            diff_node(
                "same-host", "/dup", uid="L-host", host=True, tags=["tool"],
                args=["clang", "host-clean"],
            ),
            diff_node(
                "left-target", "/dup", uid="L-target",
                args=["clang", "target-ours"],
            ),
        ]
        right = [
            diff_node(
                "same-host", "/dup", uid="R-host", host=True, tags=["tool"],
                args=["clang", "host-clean"],
            ),
            diff_node(
                "right-target", "/dup", uid="R-target",
                args=["clang", "target-ref"],
            ),
        ]
        output = self.diff(left, right, "--pair", "/dup")
        self.assertIn("[field cmds differs]", output)
        self.assertIn("+target-ours", output)
        self.assertIn("+target-ref", output)

    def test_diff_pair_duplicate_outputs_exact_counterparts(self):
        left = [
            diff_node("L-cg2", "/dup", args=["ragel", "-CG2"]),
            diff_node("L-ct0", "/dup", args=["ragel", "-CT0"]),
        ]
        right = [
            diff_node("R-cg2", "/dup", args=["ragel", "-CG2"]),
            diff_node("R-ct0", "/dup", args=["ragel", "-CT0"]),
        ]
        output = self.diff(left, right, "--pair", "/dup")
        self.assertIn("=== pair diff for /dup ===", output)
        self.assertNotIn("[field cmds differs]", output)

    def test_diff_pair_duplicate_outputs_one_sibling_differs(self):
        left = [
            diff_node("L-a", "/dup", args=["ragel", "-CG2"]),
            diff_node("L-b", "/dup", args=["ragel", "-Bours"]),
        ]
        right = [
            diff_node("R-a", "/dup", args=["ragel", "-CG2"]),
            diff_node("R-c", "/dup", args=["ragel", "-Cref"]),
        ]
        output = self.diff(left, right, "--pair", "/dup")
        self.assertIn("[field cmds differs]", output)
        self.assertIn("+-Bours", output)
        self.assertIn("+-Cref", output)

    def test_diff_aggregate_duplicate_outputs_exact_counterparts(self):
        left = [
            diff_node("L-cg2", "/dup", args=["ragel", "-CG2"]),
            diff_node("L-ct0", "/dup", args=["ragel", "-CT0"]),
        ]
        right = [
            diff_node("R-cg2", "/dup", args=["ragel", "-CG2"]),
            diff_node("R-ct0", "/dup", args=["ragel", "-CT0"]),
        ]
        self.assertNotIn("cmds", self.diff(left, right, "--by-field"))
        self.assertNotIn("cmds:", self.diff(left, right, "--by-kind"))
        by_token = self.diff(left, right, "--by-token")
        self.assertNotIn("-CG2", by_token)
        self.assertNotIn("-CT0", by_token)

    def test_diff_aggregate_duplicate_outputs_one_sibling_differs(self):
        left = [
            diff_node("L-a", "/dup", args=["ragel", "-CG2"]),
            diff_node("L-b", "/dup", args=["ragel", "-Bours"]),
        ]
        right = [
            diff_node("R-a", "/dup", args=["ragel", "-CG2"]),
            diff_node("R-c", "/dup", args=["ragel", "-Cref"]),
        ]
        by_token = self.diff(left, right, "--by-token")
        self.assertIn("-Bours", by_token)
        self.assertIn("-Cref", by_token)
        self.assertNotIn("-CG2", by_token)
        self.assertIn("cmds", self.diff(left, right, "--by-field"))
        self.assertIn("cmds:1", self.diff(left, right, "--by-kind"))

    def test_diff_roots(self):
        left = [
            diff_node("Ps", "/p", uid="Pu", kind="", deps=["Cu"], tags=["a"]),
            diff_node("Cs", "/c", uid="Cu", kind="", tags=["a"]),
        ]
        right = [
            diff_node("Ps2", "/p", uid="Pu2", kind="", deps=["Cu2"]),
            diff_node("Cs2", "/c", uid="Cu2", kind=""),
        ]
        output = self.diff(left, right, "--roots")
        self.assertIn("\n/c\n", output)
        self.assertNotIn("/p", output.splitlines())

    def test_diff_roots_dedup_duplicate_outputs_by_variant(self):
        left = [
            diff_node("same-host", "/dup", uid="L-host", host=True, tags=["tool"]),
            diff_node("left-target", "/dup", uid="L-target"),
        ]
        right = [
            diff_node("same-host", "/dup", uid="R-host", host=True, tags=["tool"]),
            diff_node("right-target", "/dup", uid="R-target"),
        ]
        output = self.diff(left, right, "--roots")
        self.assertIn(
            "=== roots: 1 leaf-most divergent outputs (of 1 divergent) ===",
            output,
        )
        self.assertEqual(output.splitlines().count("/dup"), 1)

    def test_diff_roots_partial_overlap_multi_output_node(self):
        left = [diff_node("left", "/a", uid="L", outputs=["/a", "/b"])]
        right = [diff_node("right", "/a", uid="R")]
        output = self.diff(left, right, "--roots")
        self.assertIn(
            "=== roots: 1 leaf-most divergent outputs (of 1 divergent) ===",
            output,
        )
        self.assertIn("\n/a\n", output)
        self.assertNotIn("\n/b\n", output)

    def test_diff_by_token_roots(self):
        left = [
            diff_node(
                "LP", "/p", uid="P", kind="CC", deps=["C"],
                args=["cc", "PARENT_OURS"],
            ),
            diff_node("LC", "/c", uid="C", kind="CC", args=["cc", "CHILD_OURS"]),
        ]
        right = [
            diff_node(
                "RP", "/p", uid="P", kind="CC", deps=["C"],
                args=["cc", "PARENT_REF"],
            ),
            diff_node("RC", "/c", uid="C", kind="CC", args=["cc", "CHILD_REF"]),
        ]
        output = self.diff(left, right, "--by-token", "--roots")
        self.assertIn("CHILD_OURS", output)
        self.assertIn("CHILD_REF", output)
        self.assertNotIn("PARENT_OURS", output)
        self.assertNotIn("PARENT_REF", output)

    def test_diff_by_token_group(self):
        left = [
            diff_node("LA", "$(B)/dirA/a.o", kind="CC", args=["cc", "TOKA_OURS"]),
            diff_node("LB", "$(B)/dirB/b.o", kind="PB", args=["cc", "TOKB_OURS"]),
        ]
        right = [
            diff_node("RA", "$(B)/dirA/a.o", kind="CC", args=["cc", "TOKA_REF"]),
            diff_node("RB", "$(B)/dirB/b.o", kind="PB", args=["cc", "TOKB_REF"]),
        ]
        output = self.diff(left, right, "--by-token", "--group", "kind,dir")
        for expected in (
            "kind=CC dir=dirA",
            "kind=PB dir=dirB",
            "TOKA_OURS",
            "TOKB_REF",
        ):
            self.assertIn(expected, output)
        cc_index = output.index("kind=CC dir=dirA")
        pb_index = output.index("kind=PB dir=dirB")
        low, high = sorted((cc_index, pb_index))
        token_a_in_first = low < output.index("TOKA_OURS") < high
        token_b_in_first = low < output.index("TOKB_OURS") < high
        self.assertNotEqual(token_a_in_first, token_b_in_first)

    def test_diff_pair_structured_cmds(self):
        left = [diff_node(
            "L", "/s", kind="CC",
            cmds=[{"cmd_args": ["cc", "-c", "a", "b"], "cwd": "/wd_ours"}],
        )]
        right = [diff_node(
            "R", "/s", kind="CC",
            cmds=[{"cmd_args": ["cc", "-c", "b", "a"], "cwd": "/wd_ref"}],
        )]
        output = self.diff(left, right, "--pair", "/s")
        self.assertIn("[field cmds differs]", output)
        self.assertIn("cwd: ours=/wd_ours ref=/wd_ref", output)
        self.assertIn("arg order", output)
        self.assertIn("ours: cc -c a b", output)
        self.assertIn("ref:  cc -c b a", output)

    def test_canon_inputs_archive_by_keys_ignores_key_list_basename(self):
        node = raw_node(
            "archive",
            "AR",
            "$(B)/pkg/LuaScripts.inc",
            inputs=[
                "$(B)/mod/a.raw",
                "$(B)/mod/sub/b.raw",
                "$(S)/mod/a.lua",
                "$(S)/mod/sub/b.lua",
                "$(B)/tools/archiver/archiver",
            ],
            cmds=[{"cmd_args": [
                "$(B)/tools/archiver/archiver", "-q", "-x", "-p",
                "$(B)/mod/a.raw", "$(B)/mod/sub/b.raw",
                "-k", "a.lua:sub/b.lua",
                "-o", "$(B)/pkg/LuaScripts.inc",
            ]}],
        )
        normalized = self.normalized_special_node(node)
        self.assertEqual(normalized["inputs"], [
            "$(B)/mod/a.raw",
            "$(B)/mod/sub/b.raw",
            "$(B)/tools/archiver/archiver",
        ])

    def test_canon_inputs_cython_drops_generated_c_include_closure(self):
        node = raw_node(
            "cython",
            "CY",
            "$(B)/pkg/mod.pyx.cpp",
            inputs=[
                "$(B)/contrib/tools/python3/python3",
                "$(S)/contrib/tools/cython/cython.py",
                "$(S)/contrib/tools/cython/Cython/Utility/UFuncs_C.c",
                "$(S)/pkg/mod.pyx",
                "$(S)/pkg/dep.pxd",
                "$(S)/pkg/inc.pxi",
                "$(S)/pkg/helper.py",
                "$(S)/build/scripts/cpp_proto_wrapper.py",
                "$(S)/pkg/extern.h",
                "$(S)/contrib/python/numpy/include/numpy/arrayobject.h",
                "$(S)/contrib/libs/cxxsupp/libcxx/include/vector",
            ],
            cmds=[{"cmd_args": [
                "$(B)/contrib/tools/python3/python3",
                "$(S)/contrib/tools/cython/cython.py",
                "$(S)/pkg/mod.pyx",
                "-o", "$(B)/pkg/mod.pyx.cpp",
            ]}],
        )
        normalized = self.normalized_special_node(node)
        self.assertEqual(normalized["inputs"], [
            "$(B)/contrib/tools/python3/python3",
            "$(S)/contrib/tools/cython/Cython/Utility/UFuncs_C.c",
            "$(S)/contrib/tools/cython/cython.py",
            "$(S)/pkg/dep.pxd",
            "$(S)/pkg/helper.py",
            "$(S)/pkg/inc.pxi",
            "$(S)/pkg/mod.pyx",
        ])

    def test_canon_inputs_raw_aux_drops_compile_closure(self):
        node = raw_node(
            "raw-aux",
            "PR",
            "$(B)/pkg/012345_raw.auxcpp",
            inputs=[
                "$(B)/tools/rescompiler/rescompiler",
                "$(B)/pkg/mod.py",
                "$(S)/build/scripts/gen_py_protos.py",
                "$(S)/pkg/mod.proto",
                "$(S)/pkg/payload.h",
                "$(S)/pkg/transitive.h",
                "$(S)/contrib/libs/cxxsupp/libcxx/include/vector",
                "$(S)/contrib/tools/swig/Lib/python/typemaps.i",
                "$(S)/util/system/thread.i",
            ],
            cmds=[{"cmd_args": [
                "$(B)/tools/rescompiler/rescompiler",
                "$(B)/pkg/012345_raw.auxcpp",
                "$(B)/pkg/mod.py", "-resfs/file/py/pkg/mod.py",
                "$(S)/pkg/payload.h", "-resfs/file/payload.h",
            ]}],
        )
        normalized = self.normalized_special_node(node)
        self.assertEqual(normalized["inputs"], [
            "$(B)/pkg/mod.py",
            "$(B)/tools/rescompiler/rescompiler",
            "$(S)/pkg/payload.h",
        ])

    def test_canon_inputs_enum_parser_keeps_only_action_inputs(self):
        node = raw_node(
            "enum",
            "EN",
            "$(B)/pkg/mode.h_serialized.cpp",
            inputs=[
                "$(B)/tools/enum_parser/enum_parser",
                "$(S)/pkg/mode.h",
                "$(S)/pkg/dep.h",
                "$(S)/util/generic/serialized_enum.h",
                "$(S)/contrib/libs/cxxsupp/libcxx/include/vector",
            ],
            cmds=[{"cmd_args": [
                "$(B)/tools/enum_parser/enum_parser",
                "$(S)/pkg/mode.h",
                "--include-path", "pkg/mode.h",
                "--output", "$(B)/pkg/mode.h_serialized.cpp",
            ]}],
        )
        normalized = self.normalized_special_node(node)
        self.assertEqual(normalized["inputs"], [
            "$(B)/tools/enum_parser/enum_parser",
            "$(S)/pkg/mode.h",
        ])

    def test_diff_pair_pic_and_non_pic_variants_do_not_mispair(self):
        left = [
            diff_node(
                "l1", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.o"],
                args=["ar", "$(B)/x/foo.cpp.o"],
            ),
            diff_node(
                "l2", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.pic.o"],
                args=["ar", "$(B)/x/foo.cpp.pic.o"],
            ),
        ]
        right = [
            diff_node(
                "r1", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.o"],
                args=["ar", "$(B)/x/foo.cpp.o"],
            ),
            diff_node(
                "r2", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.pic.o"],
                args=["ar", "$(B)/x/foo.cpp.pic.o"],
            ),
        ]
        self.assertNotIn("differs", self.diff(left, right, "--pair", "$(B)/x/lib.a"))

    def test_diff_pair_pic_variant_member_divergence_is_reported(self):
        left = [
            diff_node(
                "l1", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.o"],
                args=["ar", "$(B)/x/foo.cpp.o"],
            ),
            diff_node(
                "l2", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.pic.o"],
                args=["ar", "$(B)/x/foo.cpp.pic.o"],
            ),
        ]
        right = [
            diff_node(
                "r1", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.o"],
                args=["ar", "$(B)/x/foo.cpp.o"],
            ),
            diff_node(
                "r2", "$(B)/x/lib.a", kind="AR",
                inputs=["$(B)/x/foo.cpp.pic.o", "$(B)/x/bar.cpp.pic.o"],
                args=["ar", "$(B)/x/foo.cpp.pic.o", "$(B)/x/bar.cpp.pic.o"],
            ),
        ]
        output = self.diff(left, right, "--pair", "$(B)/x/lib.a")
        self.assertIn("bar.cpp.pic.o", output)


if __name__ == "__main__":
    unittest.main(verbosity=2)
