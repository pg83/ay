import unittest

from tst import lib


RESOURCE_LIBRARY = (
    "LIBRARY()\n"
    "NO_LIBC()\n"
    "NO_RUNTIME()\n"
    "NO_UTIL()\n"
    "END()\n"
)


def base_files():
    files = {
        "library/cpp/resource/ya.make": RESOURCE_LIBRARY,
        "dep/ya.make": (
            "LIBRARY()\n"
            "NO_LIBC()\n"
            "NO_RUNTIME()\n"
            "NO_UTIL()\n"
            "SRCS(d.cpp)\n"
            "END()\n"
        ),
        "dep/d.cpp": "int d(){return 0;}\n",
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    return files


class EmitBundleTest(unittest.TestCase):
    def test_bundle_output_selects_secondary_docs_output_and_renames_it(self):
        files = base_files()
        files.update({
            "guide/ya.make": "DOCS(book)\nEND()\n",
            "guide/index.md": "# Guide\n",
            "cons/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                            "BUNDLE_OUTPUT(guide preprocessed.tar.gz RENAME input.tar.gz)\n"
                            "FROM_ARCHIVE(input.tar.gz OUT_NOAUTO index.md)\nRESOURCE(index.md key)\nEND()\n",
        })
        graph = lib.make(files, "cons")
        bundle = lib.node_by_output(graph, "$(B)/cons/input.tar.gz")
        book = lib.node_by_output(graph, "$(B)/guide/book.tar.gz")
        self.assertEqual(bundle["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/bundle_output.py", "--result", "$(B)/guide/book.tar.gz",
            "--name", "preprocessed.tar.gz", "--output", "$(B)/cons/input.tar.gz",
        ])
        self.assertIn(book["uid"], bundle["deps"])
        extracted = lib.node_by_output(graph, "$(B)/cons/index.md")
        self.assertIn(bundle["uid"], extracted["deps"])
        self.assertIn("$(B)/cons/input.tar.gz", extracted["inputs"])

    def test_suffix_changes_the_command_path_but_keeps_the_primary_result_input(self):
        files = base_files()
        files["app/ya.make"] = f"PROGRAM(app)\n{lib.BARE_MODULE}USE_SDC_META()\nEND()\n"
        for suffix in ("", ".sdcmeta"):
            with self.subTest(suffix=suffix):
                files["pkg/ya.make"] = f"UNION()\nBUNDLE(app SUFFIX {suffix} NAME artifact)\nEND()\n" if suffix else "UNION()\nBUNDLE(app NAME artifact)\nEND()\n"
                graph = lib.make(files, "pkg")
                bundle = lib.node_by_output(graph, "$(B)/pkg/artifact")
                producer = lib.node_by_output(graph, "$(B)/app/app")
                self.assertEqual(bundle["cmds"][0]["cmd_args"][-2:], ["$(B)/app/app" + suffix, "$(B)/pkg/artifact"])
                self.assertEqual([p for p in bundle["inputs"] if p.startswith("$(B)/")], ["$(B)/app/app"])
                self.assertIn(producer["uid"], bundle["deps"])

    def test_generated_file_wires_producer_dependency(self):
        files = base_files()
        files.update({
            "cons/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(c.cpp)\n"
                "BUNDLE(dep NAME x.bundle)\n"
                "RESOURCE(x.bundle key)\n"
                "END()\n"
            ),
            "cons/c.cpp": "int c(){return 0;}\n",
        })
        graph = lib.make(files, "cons")
        archive = lib.node_by_output(graph, "$(B)/dep/libdep.a")
        bundle = lib.node_by_output(graph, "$(B)/cons/x.bundle")
        self.assertEqual(bundle["kv"]["p"], "BN")
        self.assertIn("$(B)/dep/libdep.a", bundle["inputs"])
        self.assertIn(
            "fs_tools.py rename $(B)/dep/libdep.a $(B)/cons/x.bundle",
            " ".join(bundle["cmds"][0]["cmd_args"]),
        )
        self.assertIn(archive["uid"], bundle["deps"])

        objcopy = lib.node_by_output_prefix(graph, "$(B)/cons/objcopy_")
        self.assertIn("$(B)/cons/x.bundle", objcopy["inputs"])
        self.assertNotIn("$(S)/cons/x.bundle", objcopy["inputs"])
        self.assertIn(bundle["uid"], objcopy["deps"])

    def test_program_attributes_fs_tools_to_module(self):
        files = base_files()
        files.update({
            "build/scripts/fs_tools.py": "import process_command_files as pcf\n",
            "build/scripts/process_command_files.py": "\n",
            "blib/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(b.cpp)\n"
                "BUNDLE(dep NAME y.bundle)\n"
                "RESOURCE(y.bundle blib/key)\n"
                "END()\n"
            ),
            "blib/b.cpp": "int b(){return 0;}\n",
            "prog/ya.make": (
                "PROGRAM()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(main.cpp)\n"
                "PEERDIR(blib)\n"
                "BUNDLE(dep NAME x.bundle)\n"
                "RESOURCE(x.bundle dep/key)\n"
                "END()\n"
            ),
            "prog/main.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog", opensource=False)
        fs_tools = "$(S)/build/scripts/fs_tools.py"
        link = lib.node_by_output(graph, "$(B)/prog/prog")
        self.assertIn(fs_tools, link["inputs"])

        archive = lib.node_by_output(graph, "$(B)/dep/libdep.a")
        bundle = lib.node_by_output(graph, "$(B)/prog/x.bundle")
        self.assertIn(fs_tools, bundle["inputs"])
        self.assertIn(archive["uid"], bundle["deps"])

        objcopy = lib.node_by_output_prefix(graph, "$(B)/prog/objcopy_")
        self.assertIn("$(B)/prog/x.bundle", objcopy["inputs"])
        self.assertIn(objcopy["uid"], link["deps"])

        blib_archive = lib.node_by_output(graph, "$(B)/blib/libblib.a")
        self.assertNotIn(fs_tools, blib_archive["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
