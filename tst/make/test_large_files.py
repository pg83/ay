import json
import tempfile
import unittest
from pathlib import Path

from tst import lib


MODULE = "orchard/lexicon"


def descriptor(resource, storage="SANDBOX"):
    return json.dumps({"storage": storage, "resource_id": resource})


def union(body):
    return "UNION()\n" + body + "\nEND()\n"


class LargeFilesTest(unittest.TestCase):
    def test_local_placeholder_nested_and_uppercase_match_explicit_producers(self):
        body = (
            "LARGE_FILES(AUTOUPDATED fresh.dat nested/remote.dat UPPERCASE)\n"
            "COPY_FILE(${BINDIR}/nested/remote.dat consumed.dat)\n"
        )
        files = {
            MODULE + "/ya.make": union(body),
            MODULE + "/fresh.dat": "local wins over malformed placeholder\n",
            MODULE + "/fresh.dat.external": "malformed",
            MODULE + "/nested/remote.dat.external": descriptor(7263915821, "sAnDbOx"),
            MODULE + "/UPPERCASE.external": descriptor("7263915822"),
        }
        actual = lib.make(files, MODULE)
        files[MODULE + "/ya.make"] = union(body.replace(
            "LARGE_FILES(AUTOUPDATED fresh.dat nested/remote.dat UPPERCASE)",
            "COPY_FILE(fresh.dat fresh.dat)\n"
            "FROM_SANDBOX(FILE 7263915821 OUT_NOAUTO nested/remote.dat RENAME remote.dat)\n"
            "FROM_SANDBOX(FILE 7263915822 OUT_NOAUTO UPPERCASE)",
        ))
        self.assertEqual(actual, lib.make(files, MODULE))
        local = lib.node_by_output(actual, f"$(B)/{MODULE}/fresh.dat")
        self.assertEqual(local["inputs"], [f"$(S)/{MODULE}/fresh.dat"])
        remote = lib.node_by_output(actual, f"$(B)/{MODULE}/nested/remote.dat")
        self.assertEqual(remote["outputs"], [f"$(B)/{MODULE}/nested/remote.dat"])
        self.assertIn("--copy-to-dir", remote["cmds"][0]["cmd_args"])
        self.assertIn("--rename", remote["cmds"][0]["cmd_args"])
        self.assertFalse(any(name.endswith(".external") for name in remote["inputs"]))
        consumer = lib.node_by_output(actual, f"$(B)/{MODULE}/consumed.dat")
        self.assertIn(remote["uid"], consumer["deps"])

    def test_empty_and_misplaced_autoupdated(self):
        result = lib.make_process({
            MODULE + "/ya.make": union(
                "LARGE_FILES()\nLARGE_FILES(AUTOUPDATED)\n"
                "LARGE_FILES(AUTOUPDATED note.dat AUTOUPDATED second.dat)"
            ),
            MODULE + "/note.dat": "first\n",
            MODULE + "/second.dat": "second\n",
            "build/sysincl/macro.yml": "- odd_key: 1\n  includes:\n    - vector\n",
        }, MODULE)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Please set AUTOUPDATED argument before other file names", result.stderr)
        self.assertNotIn("unrecognised record key", result.stderr)
        graph = json.loads(result.stdout)
        for name in ("note.dat", "second.dat"):
            self.assertEqual(lib.node_by_output(graph, f"$(B)/{MODULE}/{name}")["kv"]["p"], "CP")
        self.assertFalse(any(output.endswith("/AUTOUPDATED") for node in graph["graph"] for output in node["outputs"]))

    def test_source_resolution_order_and_typed_roots(self):
        files = {
            MODULE + "/ya.make": union(
                "SRCDIR(grove/store)\n"
                "LARGE_FILES(local.dat borrowed.dat root.dat "
                "${ARCADIA_ROOT}/explicit.dat ${CURDIR}/typed.dat "
                "${BINDIR}/download.dat)"
            ),
            MODULE + "/local.dat": "module first\n",
            "grove/store/local.dat": "shadowed\n",
            "grove/store/borrowed.dat": "srcdir\n",
            "root.dat": "root\n",
            "explicit.dat": "explicit\n",
            MODULE + "/typed.dat": "typed\n",
            MODULE + "/download.dat.external": descriptor(7263915823),
        }
        # A build-rooted placeholder cannot be opened during configure.
        result = lib.make_process(files, MODULE)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("neither actual data nor placeholder", result.stderr)
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
            " ${BINDIR}/download.dat", "")
        graph = lib.make(files, MODULE)
        for source, output in (
            (MODULE + "/local.dat", MODULE + "/local.dat"),
            ("grove/store/borrowed.dat", MODULE + "/borrowed.dat"),
            ("root.dat", MODULE + "/root.dat"),
            ("explicit.dat", "explicit.dat"),
            (MODULE + "/typed.dat", MODULE + "/typed.dat"),
        ):
            self.assertEqual(lib.node_by_output(graph, "$(B)/" + output)["inputs"],
                             ["$(S)/" + source])

    def test_shared_lexical_include_uses_each_module_environment_and_source(self):
        files = {"shared/large.inc": "LARGE_FILES($PAYLOAD)\nSET(PAYLOAD later.dat)\n"}
        for name, payload in (("copper", "nested/remote.dat"), ("indigo", "LOCAL")):
            module = "grove/" + name
            files[module + "/ya.make"] = union(
                f"SET(PAYLOAD {payload})\nINCLUDE(${{ARCADIA_ROOT}}/shared/large.inc)")
        files["grove/copper/nested/remote.dat.external"] = descriptor(7263915824)
        files["grove/indigo/LOCAL"] = "local\n"
        for targets in (("grove/copper", "grove/indigo"), ("grove/indigo", "grove/copper")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                self.assertEqual(lib.node_by_output(graph, "$(B)/grove/copper/nested/remote.dat")["kv"]["p"], "SB")
                self.assertEqual(lib.node_by_output(graph, "$(B)/grove/indigo/LOCAL")["kv"]["p"], "CP")
                self.assertFalse(any("later.dat" in output for node in graph["graph"] for output in node["outputs"]))

    def test_descriptor_values_keep_python_json_and_string_semantics(self):
        for value in (987654321098765432109876543210, "renamed-id", True, False, None,
                      [1, "two", None], {"part": "two", "flag": True}, 2.75):
            with self.subTest(value=value):
                graph = lib.make({
                    MODULE + "/ya.make": union("LARGE_FILES(value.dat)"),
                    MODULE + "/value.dat.external": descriptor(value),
                }, MODULE)
                args = lib.node_by_output(graph, f"$(B)/{MODULE}/value.dat")["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--resource-id") + 1], str(value))

    def test_placeholders_resolve_from_srcdir_and_source_root(self):
        graph = lib.make({
            MODULE + "/ya.make": union(
                "SRCDIR(${ARCADIA_BUILD_ROOT}/grove/generated grove/stock)\n"
                "LARGE_FILES(borrowed.dat rooted.dat)"),
            "grove/stock/borrowed.dat.external": descriptor(7263915828),
            "rooted.dat.external": descriptor(7263915829),
        }, MODULE)
        for name, resource in (("borrowed.dat", "7263915828"), ("rooted.dat", "7263915829")):
            args = lib.node_by_output(graph, f"$(B)/{MODULE}/{name}")["cmds"][0]["cmd_args"]
            self.assertEqual(args[args.index("--resource-id") + 1], resource)

    def test_host_and_target_contexts_keep_their_own_producers(self):
        graph = lib.make({
            "grove/compiler/ya.make": (
                "PROGRAM()\n" + lib.BARE_MODULE +
                "ADDINCL(${BINDIR})\nLARGE_FILES(payload.h)\nSRCS(main.cpp)\nEND()\n"
            ),
            "grove/compiler/main.cpp": '#include "payload.h"\nint main() {return 0;}\n',
            "grove/compiler/payload.h.external": descriptor(7263915830),
            "grove/consumer/ya.make": union("RUN_PROGRAM(grove/compiler OUT result.dat)"),
        }, "grove/compiler", "grove/consumer")
        downloads = [node for node in graph["graph"] if node["kv"]["p"] == "SB"]
        self.assertEqual(len(downloads), 2)
        self.assertEqual(len({node["platform"] for node in downloads}), 2)
        for download in downloads:
            [compiled] = [node for node in graph["graph"]
                          if node["kv"]["p"] == "CC" and "$(S)/grove/compiler/main.cpp" in node["inputs"]
                          and node["platform"] == download["platform"]]
            self.assertIn(download["uid"], compiled["deps"])

    def test_registered_build_srcdir_precedes_source_root(self):
        files = {
            "grove/producer/ya.make": union("COPY_FILE(seed.dat ${ARCADIA_BUILD_ROOT}/stock/known.dat)"),
            "grove/producer/seed.dat": "generated\n",
            MODULE + "/ya.make": union(
                "SRCDIR(${ARCADIA_BUILD_ROOT}/stock)\nLARGE_FILES(known.dat)"),
            MODULE + "/known.dat.external": descriptor(7263915831),
            "known.dat": "source-root fallback\n",
        }
        # lib.make appends its first target after the additional CLI arguments.
        graph = lib.make(files, MODULE, "grove/producer")
        self.assertEqual(lib.node_by_output(graph, f"$(B)/{MODULE}/known.dat")["kv"]["p"], "SB")

    def test_symlinks_are_unresolved_but_typed_sources_pass_through(self):
        with tempfile.TemporaryDirectory(prefix="ay-large-files-") as directory:
            root = Path(directory)
            lib.seed_root(root)
            module = root / MODULE
            module.mkdir(parents=True)
            (root / "ya.conf").write_text('[flags]\nOPENSOURCE="yes"\n')
            (module / "target.dat").write_text("target\n")
            (module / "link.dat").symlink_to("target.dat")
            (module / "link.dat.external").write_text(descriptor(7263915825))
            (module / "ya.make").write_text(union("LARGE_FILES(link.dat)"))
            args = ("make", "-j0", "-G", "--sandboxing", "--source-root", root,
                    "--target-platform", "default-linux-aarch64",
                    "--host-platform", "default-linux-x86_64", MODULE)
            graph = json.loads(lib.run(*args).stdout)
            self.assertEqual(lib.node_by_output(graph, f"$(B)/{MODULE}/link.dat")["kv"]["p"], "SB")
            (module / "ya.make").write_text(union("LARGE_FILES(${CURDIR}/link.dat)"))
            graph = json.loads(lib.run(*args).stdout)
            self.assertEqual(lib.node_by_output(graph, f"$(B)/{MODULE}/link.dat")["kv"]["p"], "CP")
            (module / "ya.make").write_text(union("LARGE_FILES(link.dat)"))
            (module / "link.dat.external").unlink()
            (module / "description.json").write_text(descriptor(7263915825))
            (module / "link.dat.external").symlink_to("description.json")
            result = lib.run_process(*args)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("neither actual data nor placeholder", result.stderr)

            (module / "link.dat.external").unlink()
            (module / "link.dat.external").mkdir()
            result = lib.run_process(*args)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("error processing placeholder file", result.stderr)

            (module / "cycle").symlink_to("cycle")
            (module / "ya.make").write_text(union("LARGE_FILES(cycle/absent.dat)"))
            result = lib.run_process(*args)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("too many levels", result.stderr.lower())

    def test_placeholder_errors_are_configure_errors(self):
        for content, message in (
            (None, 'neither actual data nor placeholder is found for "absent.dat"'),
            ("not json", "error processing placeholder file"),
            ('{"resource_id": 9}', "storage"),
            ('{"storage": "SANDBOX"}', "resource_id"),
            ('{"storage": 4, "resource_id": 9}', "lower"),
            (descriptor(9, "unavailable"), "FROM_UNAVAILABLE"),
            (descriptor(9, "HTTP"), "FROM_HTTP"),
            (descriptor(9, "ARCHIVE"), "FROM_ARCHIVE"),
        ):
            with self.subTest(content=content):
                files = {MODULE + "/ya.make": union("LARGE_FILES(absent.dat)")}
                if content is not None:
                    files[MODULE + "/absent.dat.external"] = content
                result = lib.make_process(files, MODULE)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_resource_consumer_uses_noauto_outputs_without_compiling_sources(self):
        files = {
            MODULE + "/ya.make": (
                "LIBRARY()\n" + lib.BARE_MODULE +
                "LARGE_FILES(nested/embedded.cpp unused.cpp)\n"
                "RESOURCE(${BINDIR}/nested/embedded.cpp orchard-key)\nEND()\n"
            ),
            MODULE + "/nested/embedded.cpp.external": descriptor(7263915826),
            MODULE + "/unused.cpp.external": descriptor(7263915827),
            "library/cpp/resource/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "END()\n",
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, MODULE)
        [download] = [node for node in graph["graph"] if node["kv"]["p"] == "SB"]
        resource = lib.node_by_output_prefix(graph, f"$(B)/{MODULE}/objcopy_")
        self.assertIn(download["uid"], resource["deps"])
        self.assertIn(f"$(B)/{MODULE}/nested/embedded.cpp", resource["inputs"])
        self.assertFalse(any(output.endswith("embedded.cpp.o") for node in graph["graph"] for output in node["outputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
