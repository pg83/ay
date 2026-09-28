import base64
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
