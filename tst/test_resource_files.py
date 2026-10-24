import base64
import hashlib
import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def base_files(body):
    files = {
        "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
        "lib/ya.make": f"LIBRARY()\n{BARE}{body}END()\n",
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    return files


def objcopy_nodes(graph):
    return [
        node for node in graph["graph"]
        if node["kv"]["p"] == "PY"
        and node["outputs"][0].startswith("$(B)/lib/objcopy_")
    ]


def section(args, flag):
    start = args.index(flag) + 1
    end = start
    while end < len(args) and not args[end].startswith("--"):
        end += 1
    return args[start:end]


def payload(node):
    args = node["cmds"][0]["cmd_args"]
    inputs = section(args, "--inputs") if "--inputs" in args else []
    keys = [
        base64.b64decode(key).decode()
        for key in (section(args, "--keys") if "--keys" in args else [])
    ]
    kvs = section(args, "--kvs") if "--kvs" in args else []
    return inputs, keys, kvs


def payloads(graph):
    return [payload(node) for node in objcopy_nodes(graph)]


def make_error(files):
    try:
        lib.make(files, "lib")
    except AssertionError as error:
        return str(error)
    raise AssertionError("ay make unexpectedly succeeded")


class ResourceFilesTest(unittest.TestCase):
    def test_plugin_resource_peer_follows_explicit_peers(self):
        for macro in ("RESOURCE_FILES(data.txt)", "ALL_RESOURCE_FILES(txt .)"):
            with self.subTest(macro=macro):
                files = base_files(f"{macro}\nPEERDIR(peer)\nSRCS(a.cpp)\n")
                files.update({
                    "lib/data.txt": "",
                    "lib/a.cpp": "",
                    "peer/ya.make": f"LIBRARY()\n{BARE}ADDINCL(GLOBAL peer)\nEND()\n",
                    "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}ADDINCL(GLOBAL library/cpp/resource)\nEND()\n",
                })
                graph = lib.make(files, "lib", "--sandboxing")
                compiled = lib.node_by_output(graph, "$(B)/lib/a.cpp.o")
                includes = [a for a in compiled["cmds"][0]["cmd_args"] if a.startswith("-I")]
                self.assertLess(includes.index("-I$(S)/peer"), includes.index("-I$(S)/library/cpp/resource"))

    def test_plugin_hash_preserves_module_root_variables(self):
        for root, resolved in (("BINDIR", "$(B)/lib"), ("CURDIR", "$(S)/lib")):
            with self.subTest(root=root):
                raw = "${" + root + "}/a.txt"
                files = base_files(
                    "SET(RESOURCE_PREFIX assets/)\n"
                    "COPY_FILE(lib/input.txt a.txt)\n"
                    f"RESOURCE_FILES(PREFIX $RESOURCE_PREFIX STRIP ${{{root}}}/ {raw})\n"
                )
                files["lib/input.txt"] = "generated"
                files["lib/a.txt"] = "source"
                graph = lib.make(files, "lib")
                key = "resfs/file/assets/a.txt"
                seed = [raw, base64.b64encode(key.encode()).decode(),
                        'resfs/src/' + key + '=${rootrel;context=TEXT;input=TEXT:"' + raw + '"}',
                        "$S/lib"]
                digest = hashlib.md5(",".join(sorted(seed)).encode()).hexdigest()[:26]
                node = lib.node_by_output(graph, f"$(B)/lib/objcopy_{digest}.o")
                self.assertEqual(payload(node), ([resolved + "/a.txt"], [key],
                                               ["resfs/src/" + key + "=lib/a.txt"]))

    def test_source_root_keys_and_digest_bind_after_plugin_expansion(self):
        for form in ("literal", "strip", "scalar", "recursive", "append", "default", "last", "include"):
            with self.subTest(form=form):
                raw = "${ARCADIA_ROOT}/lib/data.txt"
                key = "resfs/file/" + raw
                body = "RESOURCE_FILES(" + raw + ")\n"
                if form == "strip":
                    key = "resfs/file/assets/data.txt"
                    body = "RESOURCE_FILES(STRIP ${ARCADIA_ROOT}/lib/ PREFIX assets/ " + raw + ")\n"
                elif form in ("scalar", "recursive", "append", "default", "last", "include"):
                    key = "resfs/file/final.txt"
                    body = "SET(FILE " + raw + ")\n"
                    if form == "recursive":
                        body = "SET(BASE ${ARCADIA_ROOT}/lib)\nSET(FILE ${BASE}/data.txt)\n"
                    if form == "append":
                        body = "SET(FILE)\nSET_APPEND(FILE " + raw + ")\n"
                    elif form == "default":
                        body = "DEFAULT(FILE " + raw + ")\n"
                    elif form == "last":
                        body = "SET(FILE missing.txt)\n" + body + "DEFAULT(FILE other.txt)\n"
                    elif form == "include":
                        body = "INCLUDE(roots.inc)\n"
                    body += "RESOURCE_FILES(DEST final.txt ${FILE})\n"
                files = base_files(body)
                files["lib/data.txt"] = "payload\n"
                files["lib/roots.inc"] = "SET(BASE ${ARCADIA_ROOT}/lib)\nSET(FILE ${BASE}/data.txt)\n"
                graph = lib.make(files, "lib", "--sandboxing")
                seed = [raw, base64.b64encode(key.encode()).decode(),
                        'resfs/src/' + key + '=${rootrel;context=TEXT;input=TEXT:"' + raw + '"}',
                        "$S/lib"]
                digest = hashlib.md5(",".join(sorted(seed)).encode()).hexdigest()[:26]
                node = lib.node_by_output(graph, f"$(B)/lib/objcopy_{digest}.o")
                runtime_key = key.replace("${ARCADIA_ROOT}", "$(S)")
                self.assertEqual(payload(node), (["$(S)/lib/data.txt"], [key],
                                               ["resfs/src/" + runtime_key + "=lib/data.txt"]))
                self.assertCountEqual(node["inputs"], [
                    "$(B)/tools/rescompiler/rescompiler", "$(B)/tools/rescompressor/rescompressor",
                    "$(S)/lib/data.txt", "$(S)/build/scripts/objcopy.py",
                ])
                archive = lib.node_by_output(graph, "$(B)/lib/liblib.global.a")
                self.assertIn(node["uid"], archive["deps"])
                self.assertIn(node["outputs"][0], archive["inputs"])

    def test_appended_root_words_keep_keys_and_physical_consumer_bindings(self):
        files = base_files(
            "SET(FILES ${ARCADIA_ROOT}/lib/a.txt)\n"
            "SET_APPEND(FILES ${ARCADIA_ROOT}/lib/b.txt)\n"
            "RESOURCE_FILES(STRIP ${ARCADIA_ROOT}/lib/ PREFIX assets/ ${FILES})\n"
        )
        files.update({"lib/a.txt": "a", "lib/b.txt": "b"})
        graph = lib.make(files, "lib", "--sandboxing")
        [node] = objcopy_nodes(graph)
        self.assertEqual(payload(node), (
            ["$(S)/lib/a.txt", "$(S)/lib/b.txt"],
            ["resfs/file/assets/a.txt", "resfs/file/assets/b.txt"],
            ["resfs/src/resfs/file/assets/a.txt=lib/a.txt",
             "resfs/src/resfs/file/assets/b.txt=lib/b.txt"],
        ))
        self.assertIn("$(S)/lib/a.txt", node["inputs"])
        self.assertIn("$(S)/lib/b.txt", node["inputs"])

    def test_build_root_uses_late_generated_input_and_raw_resource_action(self):
        raw = "${ARCADIA_BUILD_ROOT}/lib/copy.txt"
        key = "resfs/file/copy.txt"
        files = base_files(
            "COPY_FILE(lib/data.txt copy.txt)\n"
            "RESOURCE_FILES(STRIP ${ARCADIA_BUILD_ROOT}/lib/ " + raw + ")\n"
        )
        for tool in ("contrib/tools/yasm", "tools/rorescompiler"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[1])
        files["lib/data.txt"] = "payload\n"
        graph = lib.make(files, "lib", "--sandboxing",
                         "--target-platform", "default-linux-x86_64")
        self.assertEqual(objcopy_nodes(graph), [])
        def digest(values):
            return hashlib.md5(",".join(sorted(values + ["$S/lib"])).encode()).hexdigest()[:26]
        kv = 'resfs/src/' + key + '=${rootrel;context=TEXT;input=TEXT:"' + raw + '"}'
        path_label = "ro_" + digest([raw, key])
        kv_label = "ro_" + digest(["-", kv])
        asm = digest(["-", kv.split("=", 1)[1], kv_label + ".rodata", raw, path_label + ".rodata"])
        cpp = digest([kv_label + "=resfs/src/" + key, path_label + "=" + key])
        compressed = lib.node_by_output(graph, "$(B)/lib/" + asm + ".yasm")
        generated = lib.node_by_output(graph, "$(B)/lib/copy.txt")
        command = compressed["cmds"][0]["cmd_args"]
        self.assertIn("$(B)/lib/copy.txt", command)
        self.assertIn("$(B)/lib/copy.txt", compressed["inputs"])
        self.assertIn(generated["uid"], compressed["deps"])
        source = lib.node_by_output(graph, "$(B)/lib/" + cpp + ".auxcpp")
        self.assertEqual(source["cmds"][0]["cmd_args"][-2:],
                         [kv_label + "=resfs/src/" + key, path_label + "=" + key])

    def test_prefix_dest_and_strip_forms(self):
        files = base_files(
            "RESOURCE_FILES(PREFIX pfx/ data/a.txt data/b.txt)\n"
            "RESOURCE_FILES(DEST dst/x.txt data/a.txt)\n"
            "RESOURCE_FILES(DONT_COMPRESS STRIP data/ data/c.txt other/d.txt)\n"
        )
        for name in ("a", "b", "c"):
            files[f"lib/data/{name}.txt"] = name
        files["lib/other/d.txt"] = "d"
        graph = lib.make(files, "lib")
        self.assertEqual(payloads(graph), [
            (
                ["$(S)/lib/data/a.txt", "$(S)/lib/data/b.txt"],
                ["resfs/file/pfx/data/a.txt", "resfs/file/pfx/data/b.txt"],
                [
                    "resfs/src/resfs/file/pfx/data/a.txt=lib/data/a.txt",
                    "resfs/src/resfs/file/pfx/data/b.txt=lib/data/b.txt",
                ],
            ),
            (
                ["$(S)/lib/data/a.txt"],
                ["resfs/file/dst/x.txt"],
                ["resfs/src/resfs/file/dst/x.txt=lib/data/a.txt"],
            ),
            (
                ["$(S)/lib/data/c.txt", "$(S)/lib/other/d.txt"],
                ["resfs/file/c.txt", "resfs/file/other/d.txt"],
                [
                    "resfs/src/resfs/file/c.txt=lib/data/c.txt",
                    "resfs/src/resfs/file/other/d.txt=lib/other/d.txt",
                ],
            ),
        ])
        node = objcopy_nodes(graph)[0]
        self.assertEqual(node["inputs"], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(S)/lib/data/a.txt",
            "$(S)/lib/data/b.txt",
            "$(S)/build/scripts/objcopy.py",
        ])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.global.a")
        self.assertEqual(
            [p for p in archive["inputs"] if p.startswith("$(B)/lib/objcopy_")],
            [n["outputs"][0] for n in objcopy_nodes(graph)],
        )

    def test_ya_tools_conf_lists_tool_configs_and_formulas(self):
        # build/plugins/res.py _YA_TOOLS_CONF: ya.conf.json stripped of its
        # dir, the tool and toolchain configs under PREFIX yatools, then one
        # RESOURCE_FILES per formula found under build/ or the conf dir.
        files = base_files("YA_TOOLS_CONF(conf)\n")
        files.update({
            "conf/ya.conf.json": (
                '{"simple_tools": {"tool_a": {}, "tool_b": {"resource": "res_b"}},'
                ' "bottles": {"bt": {"formula": "build/external_resources/bottle/resources.json"},'
                ' "far": {"formula": "elsewhere/f.json"}, "gone": {"formula": "build/external_resources/gone/resources.json"}}}'
            ),
            "conf/tools/tools/x/t.tool.json": '{"tool": {"type": "simple", "definition": {}}}',
            "conf/tools/tools/y.tool.json": '{"tool": {"type": "bundle"}}',
            "conf/tools/toolchains/tc.toolchain.json": '{"bottles": {"b2": {"formula": "conf/f.json"}}}',
            "conf/tools/internal/tiers.json": "{}",
            "conf/f.json": "{}",
            "elsewhere/f.json": "{}",
        })
        for name in ("tool_a", "res_b", "bottle", "x/t"):
            files[f"build/external_resources/{name}/resources.json"] = "{}"
        graph = lib.make(files, "lib")
        formulas = [
            "build/external_resources/bottle/resources.json",
            "build/external_resources/res_b/resources.json",
            "build/external_resources/tool_a/resources.json",
            "build/external_resources/x/t/resources.json",
            "conf/f.json",
        ]
        configs = [
            "conf/tools/internal/tiers.json",
            "conf/tools/toolchains/tc.toolchain.json",
            "conf/tools/tools/x/t.tool.json",
            "conf/tools/tools/y.tool.json",
        ]
        self.assertEqual(
            [(inputs, keys) for inputs, keys, _ in payloads(graph)],
            [
                (["$(S)/conf/ya.conf.json"], ["resfs/file/ya.conf.json"]),
                (
                    [f"$(S)/{c}" for c in configs],
                    ["resfs/file/yatools" + c[len("conf"):] for c in configs],
                ),
            ] + [([f"$(S)/{f}"], [f"resfs/file/{f}"]) for f in formulas],
        )

    def test_trailing_keyword_without_value_is_rejected(self):
        for body, message in (
            ("RESOURCE_FILES(a.txt PREFIX)", "PREFIX is the last token"),
            ("RESOURCE_FILES(DEST)", "DEST is the last token"),
            ("RESOURCE_FILES(STRIP)", "STRIP is the last token"),
        ):
            with self.subTest(body=body):
                files = base_files(body + "\n")
                files["lib/a.txt"] = "a"
                self.assertIn(message, make_error(files))

    def test_all_resource_files_by_extension(self):
        files = base_files(
            "ALL_RESOURCE_FILES(json PREFIX j/ STRIP jd/ jd jd/sub jd)\n"
            "ALL_RESOURCE_FILES(txt ${ARCADIA_ROOT}/shared /abs $UNSET_DIR/x)\n"
        )
        files.update({
            "lib/jd/x.json": "{}",
            "lib/jd/y.json": "{}",
            "lib/jd/z.txt": "",
            "lib/jd/deep.json/inner.json": "{}",
            "lib/jd/sub/q.json": "{}",
            "shared/s.txt": "",
        })
        graph = lib.make(files, "lib")
        self.assertEqual(payloads(graph), [
            (
                [
                    "$(S)/lib/jd/x.json",
                    "$(S)/lib/jd/y.json",
                    "$(S)/lib/jd/sub/q.json",
                ],
                [
                    "resfs/file/j/x.json",
                    "resfs/file/j/y.json",
                    "resfs/file/j/sub/q.json",
                ],
                [
                    "resfs/src/resfs/file/j/x.json=lib/jd/x.json",
                    "resfs/src/resfs/file/j/y.json=lib/jd/y.json",
                    "resfs/src/resfs/file/j/sub/q.json=lib/jd/sub/q.json",
                ],
            ),
            (
                ["$(S)/shared/s.txt"],
                ["resfs/file/${ARCADIA_ROOT}/shared/s.txt"],
                ["resfs/src/resfs/file/$(S)/shared/s.txt=shared/s.txt"],
            ),
        ])

    def test_all_resource_files_wildcards_and_nested_extension(self):
        files = base_files(
            "ALL_RESOURCE_FILES(j?o* d* missing)\n"
            "ALL_RESOURCE_FILES(sub/cfg dir nodir/x)\n"
        )
        files.update({
            "lib/da/x.json": "",
            "lib/da/q.jso": "",
            "lib/db/y.jsonl": "",
            "lib/db/z.jsn": "",
            "lib/db/w.xjson": "",
            "lib/dfile": "",
            "lib/dir/a.sub/cfg": "",
            "lib/dir/b.sub": "",
            "lib/dir/c.sub/other": "",
            "lib/dir/d.sub/cfg/nested": "",
        })
        graph = lib.make(files, "lib")
        self.assertEqual(
            [inputs for inputs, _, _ in payloads(graph)],
            [
                [
                    "$(S)/lib/da/q.jso",
                    "$(S)/lib/da/x.json",
                    "$(S)/lib/db/y.jsonl",
                ],
                ["$(S)/lib/dir/a.sub/cfg"],
            ],
        )

    def test_all_resource_files_from_dirs(self):
        files = base_files(
            "ALL_RESOURCE_FILES_FROM_DIRS(PREFIX fd/ STRIP fdir/ fdir)\n"
        )
        files.update({
            "lib/fdir/1.txt": "",
            "lib/fdir/2.bin": "",
            "lib/fdir/nested/3.txt": "",
        })
        graph = lib.make(files, "lib")
        self.assertEqual(payloads(graph), [(
            ["$(S)/lib/fdir/1.txt", "$(S)/lib/fdir/2.bin"],
            ["resfs/file/fd/1.txt", "resfs/file/fd/2.bin"],
            [
                "resfs/src/resfs/file/fd/1.txt=lib/fdir/1.txt",
                "resfs/src/resfs/file/fd/2.bin=lib/fdir/2.bin",
            ],
        )])

    def test_directory_named_past_the_expansion_limit(self):
        # ${D1} -> ... -> ${D8} = ${ARCADIA_ROOT}/res: the root reference is
        # left unexpanded when the chain runs out of passes.
        passes = 8
        defines = [f"-DD{i}=${{D{i + 1}}}" for i in range(1, passes)] + [f"-DD{passes}=${{ARCADIA_ROOT}}/res"]
        files = base_files("ALL_RESOURCE_FILES_FROM_DIRS(${D1})\n")
        files.update({"res/one.txt": "", "res/two.txt": ""})
        self.assertEqual(payloads(lib.make(files, "lib", *defines)), [(
            ["$(S)/res/one.txt", "$(S)/res/two.txt"],
            ["resfs/file/${ARCADIA_ROOT}/res/one.txt", "resfs/file/${ARCADIA_ROOT}/res/two.txt"],
            [
                "resfs/src/resfs/file/$(S)/res/one.txt=res/one.txt",
                "resfs/src/resfs/file/$(S)/res/two.txt=res/two.txt",
            ],
        )])

    def test_directory_with_more_files_than_interned_strings(self):
        # The glob interns every match while the deduper is borrowed, so the
        # deduper grows past the size it was reset to.
        count = 40000
        files = base_files("ALL_RESOURCE_FILES_FROM_DIRS(big)\n")
        files.update({f"lib/big/f{i:05}": "" for i in range(count)})
        inputs = [inputs for inputs, _, _ in payloads(lib.make(files, "lib", timeout=120))]
        self.assertEqual(
            sorted(path for chunk in inputs for path in chunk),
            [f"$(S)/lib/big/f{i:05}" for i in range(count)],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
