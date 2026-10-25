import unittest

from tst import lib


MODULE = "orchard/packing"
WRITER = "tools/template_writer"
SPLITTER = "tools/shard_maker"


def library(body):
    return "LIBRARY()\n" + lib.BARE_MODULE + body + "\nEND()\n"


def fixture(body, extra=None):
    files = {MODULE + "/ya.make": library(body)}
    files.update(extra or {})
    lib.tool_program(files, WRITER, "template_writer")
    lib.tool_program(files, SPLITTER, "shard_maker")
    return files


def split(prefix):
    return f"SPLIT_CODEGEN({SPLITTER} {prefix} NOrchard OUT_NUM 6)\n"


class SplitCodegenInputsTest(unittest.TestCase):
    def test_later_multioutput_run_keeps_actual_input_and_producer_main(self):
        graph = lib.make(fixture(
            "ADDINCL(${BINDIR})\nSRCS(use.cpp GLOBAL ${BINDIR}/second.cpp)\n"
            + split("first") + split("second") +
            f"RUN_PROGRAM({WRITER} --seed ${{CURDIR}}/original.inc "
            "IN ${CURDIR}/original.inc OUT_NOAUTO first.in second.in)\n",
            {
                MODULE + "/original.inc": "original generator input\n",
                MODULE + "/use.cpp": '#include "second.h"\n',
            },
        ), MODULE)
        generator = lib.node_by_output(graph, f"$(B)/{MODULE}/first.in")
        writer = lib.node_by_output(graph, f"$(B)/{WRITER}/template_writer")
        splitter = lib.node_by_output(graph, f"$(B)/{SPLITTER}/shard_maker")
        self.assertEqual(generator["outputs"], [f"$(B)/{MODULE}/first.in", f"$(B)/{MODULE}/second.in"])
        self.assertEqual(set(generator["inputs"]),
                         {f"$(B)/{WRITER}/template_writer", f"$(S)/{MODULE}/original.inc"})
        self.assertIn(writer["uid"], generator["deps"])
        for prefix in ("first", "second"):
            produced = lib.node_by_output(graph, f"$(B)/{MODULE}/{prefix}.0.cpp")
            self.assertEqual(produced["cmds"][0]["cmd_args"][1], f"$(B)/{MODULE}/{prefix}.in")
            self.assertEqual(set(produced["inputs"]), {
                f"$(B)/{SPLITTER}/shard_maker", f"$(B)/{MODULE}/first.in", f"$(B)/{MODULE}/{prefix}.in",
            })
            self.assertEqual(set(produced["deps"]), {generator["uid"], splitter["uid"]})
            self.assertEqual(produced["foreign_deps"], {"tool": [splitter["uid"]]})
            self.assertEqual(len(produced["outputs"]), 8)
            self.assertEqual(produced["cmds"][0]["cmd_args"][-3:], ["--cpp-parts", "1", "NOrchard"])
        second = lib.node_by_output(graph, f"$(B)/{MODULE}/second.0.cpp")
        for output in ("second.0.cpp.o", "second.5.cpp.o", "second.cpp.o", "use.cpp.o"):
            consumer = lib.node_by_output(graph, f"$(B)/{MODULE}/{output}")
            self.assertIn(second["uid"], consumer["deps"])
            self.assertIn(f"$(B)/{MODULE}/second.0.cpp", consumer["inputs"])
            self.assertNotIn(f"$(B)/{MODULE}/second.in", consumer["inputs"])
            self.assertNotIn(f"$(S)/{MODULE}/original.inc", consumer["inputs"])
        self.assertIn(f"$(B)/{MODULE}/second.h",
                      lib.node_by_output(graph, f"$(B)/{MODULE}/use.cpp.o")["inputs"])

    def test_copy_from_real_source_and_later_generator_chain(self):
        for generated in (False, True):
            with self.subTest(generated=generated):
                body = split("bundle")
                if generated:
                    body += "COPY_FILE(${BINDIR}/original.inc bundle.in)\n"
                    body += f"RUN_PROGRAM({WRITER} IN ${{CURDIR}}/seed.dat OUT_NOAUTO original.inc)\n"
                else:
                    body += "COPY_FILE(original.inc bundle.in)\n"
                graph = lib.make(fixture(body, {
                    MODULE + "/original.inc": "original source\n",
                    MODULE + "/seed.dat": "seed\n",
                }), MODULE)
                copied = lib.node_by_output(graph, f"$(B)/{MODULE}/bundle.in")
                self.assertEqual(copied["inputs"], [f"$({'B' if generated else 'S'})/{MODULE}/original.inc"])
                produced = lib.node_by_output(graph, f"$(B)/{MODULE}/bundle.0.cpp")
                self.assertEqual(produced["cmds"][0]["cmd_args"][1], f"$(B)/{MODULE}/bundle.in")
                self.assertIn(copied["uid"], produced["deps"])
                self.assertIn(f"$(B)/{MODULE}/bundle.in", produced["inputs"])
                if generated:
                    original = lib.node_by_output(graph, f"$(B)/{MODULE}/original.inc")
                    self.assertIn(original["uid"], copied["deps"])
                    self.assertIn(f"$(S)/{MODULE}/seed.dat", original["inputs"])

    def test_source_fallback_and_source_shadow_do_not_create_false_cycles(self):
        for producer in ("", f"RUN_PROGRAM({WRITER} IN ${{BINDIR}}/stable.h OUT_NOAUTO stable.in)\n"):
            with self.subTest(producer=bool(producer)):
                graph = lib.make(fixture(split("stable") + producer,
                                         {MODULE + "/stable.in": "physical source\n"}), MODULE)
                produced = lib.node_by_output(graph, f"$(B)/{MODULE}/stable.0.cpp")
                self.assertEqual(produced["cmds"][0]["cmd_args"][1], f"$(S)/{MODULE}/stable.in")
                self.assertEqual(set(produced["inputs"]),
                                 {f"$(S)/{MODULE}/stable.in", f"$(B)/{SPLITTER}/shard_maker"})
                self.assertFalse(any(f"$(B)/{MODULE}/stable.in" in node["outputs"] for node in graph["graph"]))

    def test_explicit_roots_preserve_source_and_build_shadows(self):
        for root, generated in (("${CURDIR}", False), ("${ARCADIA_ROOT}/" + MODULE, False),
                                ("${BINDIR}", True), ("${ARCADIA_BUILD_ROOT}/" + MODULE, True)):
            with self.subTest(root=root):
                graph = lib.make(fixture(
                    split(root + "/selected") +
                    f"RUN_PROGRAM({WRITER} IN ${{CURDIR}}/seed.dat OUT_NOAUTO selected.in)\n",
                    {MODULE + "/selected.in": "physical shadow\n", MODULE + "/seed.dat": "seed\n"},
                ), MODULE)
                produced = lib.node_by_output(graph, f"$(B)/{MODULE}/selected.0.cpp")
                input_path = f"$({'B' if generated else 'S'})/{MODULE}/selected.in"
                self.assertEqual(produced["cmds"][0]["cmd_args"][1], input_path)
                self.assertIn(input_path, produced["inputs"])
                self.assertEqual(produced["outputs"], [
                    *(f"$(B)/{MODULE}/selected.{i}.cpp" for i in range(6)),
                    f"$(B)/{MODULE}/selected.cpp", f"$(B)/{MODULE}/selected.h",
                ])
                if generated:
                    generator = lib.node_by_output(graph, f"$(B)/{MODULE}/selected.in")
                    self.assertIn(generator["uid"], produced["deps"])
                else:
                    self.assertFalse(any(f"$(B)/{MODULE}/selected.in" in node["outputs"] for node in graph["graph"]))

    def test_source_search_keeps_srcdir_and_root_inputs(self):
        for source in ("orchard/templates", ""):
            with self.subTest(source=source):
                graph = lib.make(fixture(
                    "SRCDIR(orchard/templates)\n" + split("imported"),
                    {(source + "/" if source else "") + "imported.in": "source input\n"},
                ), MODULE)
                produced = lib.node_by_output(graph, f"$(B)/{MODULE}/imported.0.cpp")
                self.assertEqual(produced["cmds"][0]["cmd_args"][1],
                                 "$(S)/" + (source + "/" if source else "") + "imported.in")

    def test_real_cycle_and_missing_explicit_build_input_are_errors(self):
        for body, message in (
            (split("cycle") + f"RUN_PROGRAM({WRITER} IN ${{BINDIR}}/cycle.h OUT_NOAUTO cycle.in)",
             "dependency cycle among generating macros"),
            (split("${BINDIR}/missing"), "no declared macro produces"),
        ):
            with self.subTest(message=message):
                result = lib.make_process(fixture(body), MODULE)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_shared_include_expands_prefix_and_inputs_in_each_module(self):
        files = {
            "shared/shard.inc": "COPY_FILE(${CURDIR}/seed.inc $PREFIX.in)\n" + split("$PREFIX"),
        }
        for name in ("copper", "indigo"):
            files[f"grove/{name}/ya.make"] = library(
                f"SET(PREFIX {name})\nINCLUDE(${{ARCADIA_ROOT}}/shared/shard.inc)")
            files[f"grove/{name}/seed.inc"] = name
        lib.tool_program(files, SPLITTER, "shard_maker")
        for targets in (("grove/copper", "grove/indigo"), ("grove/indigo", "grove/copper")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for name in ("copper", "indigo"):
                    copied = lib.node_by_output(graph, f"$(B)/grove/{name}/{name}.in")
                    produced = lib.node_by_output(graph, f"$(B)/grove/{name}/{name}.0.cpp")
                    self.assertEqual(copied["inputs"], [f"$(S)/grove/{name}/seed.inc"])
                    self.assertEqual(produced["cmds"][0]["cmd_args"][1], f"$(B)/grove/{name}/{name}.in")
                    self.assertIn(copied["uid"], produced["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
