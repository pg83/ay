import json
import unittest

import lib
from test_exec_make import Workspace


SOURCE = "$(S)/shared/pattern.h.in"
LEFT = "$(S)/includes/left/choice.h"
RIGHT = "$(S)/includes/right/choice.h"
DETAIL = "$(S)/includes/left/detail.h"
BUILD = "$(B)/vellum/"
TOOL = "$(B)/tools/reader/reader"


def workspace(body, resolve=True, module="vellum", directory="left"):
    files = {
        module + "/ya.make": ("UNION()\n" + ("SET(DONT_RESOLVE_INCLUDES no)\n" if resolve else "")
            + f"ADDINCL(${{ARCADIA_ROOT}}/includes/{directory})\n" + body + "END()\n"),
        "shared/pattern.h.in": '#include "choice.h"\n',
        "includes/left/choice.h": '#include "detail.h"\n',
        "includes/left/detail.h": "// parsed transitive input\n",
        "includes/right/choice.h": "// distinct first-owner choice\n",
    }
    lib.tool_program(files, "tools/reader", "reader")
    return files


def consumer(input="${BINDIR}/copied.h.in", kind="IN", out="result.model"):
    return (f"RUN_PROGRAM(tools/reader --consume {input} {kind} {input} "
            f"OUT_NOAUTO {out} CWD ${{BINDIR}})\n")


def copied(context=False):
    return ("COPY_FILE_WITH_CONTEXT" if context else "COPY_FILE") + "(${ARCADIA_ROOT}/shared/pattern.h.in copied.h.in)\n"


class TemplateTextEdgesTest(unittest.TestCase):
    def make(self, files, *targets):
        targets = targets or ("vellum",)
        return lib.make(files, targets[-1], *targets[:-1], "--sandboxing")

    def assert_copy(self, graph, output, source, inputs, deps):
        node = lib.node_by_output(graph, output)
        self.assertEqual(node["outputs"], [output])
        self.assertEqual(node["cmds"][0]["cmd_args"][1:], ["$(S)/build/scripts/fs_tools.py", "copy", source, output])
        self.assertEqual(len(node["cmds"]), 1)
        self.assertEqual(set(node["inputs"]), set(inputs))
        self.assertEqual(set(node["deps"]), set(deps))
        self.assertEqual(node["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
        return node

    def assert_run(self, graph, output, input, inputs, deps, module="vellum"):
        node = lib.node_by_output(graph, output)
        self.assertEqual(node["outputs"], [output])
        self.assertEqual(node["cmds"], [{"cmd_args": [TOOL, "--consume", input],
                                       "cwd": f"$(B)/{module}",
                                       "env": {"ARCADIA_ROOT_DISTBUILD": "$(S)"}}])
        self.assertEqual(set(node["inputs"]), set(inputs))
        self.assertEqual(set(node["deps"]), set(deps))
        return node

    def check_copy_chain(self, context=False, resolve=True):
        files = workspace(copied(context) + consumer()
            + "COPY_FILE(${BINDIR}/result.model final.model)\n", resolve=resolve)
        graph = self.make(files)
        parsed = [LEFT, DETAIL] if context and resolve else []
        copy = self.assert_copy(graph, BUILD + "copied.h.in", SOURCE, [SOURCE, *parsed], [])
        tool = lib.node_by_output(graph, TOOL)
        run = self.assert_run(graph, BUILD + "result.model", BUILD + "copied.h.in",
            [TOOL, BUILD + "copied.h.in", SOURCE, *parsed], [tool["uid"], copy["uid"]])
        self.assert_copy(graph, BUILD + "final.model", BUILD + "result.model",
            [BUILD + "result.model", BUILD + "copied.h.in", SOURCE, *parsed], [run["uid"], copy["uid"]])
        union = lib.node_by_output(graph, BUILD + "vellum.pkg.fake")
        self.assertIn(SOURCE, union["inputs"])
        self.assertEqual(LEFT in union["inputs"], bool(parsed))
        return graph

    def test_text_template_copy_is_opaque_through_build_consumers(self):
        self.check_copy_chain()

    def test_with_context_keeps_real_parsed_source_and_transitive_inputs(self):
        self.check_copy_chain(context=True)

    def test_noresolve_keeps_text_and_parsed_template_obligations(self):
        for context in (False, True):
            with self.subTest(context=context):
                self.check_copy_chain(context=context, resolve=False)

    def test_direct_in_and_noparse_have_distinct_source_edges(self):
        for kind, parsed in (("IN", [LEFT, DETAIL]), ("IN_NOPARSE", [])):
            with self.subTest(kind=kind):
                graph = self.make(workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in", kind)
                    + "COPY_FILE(${BINDIR}/result.model final.model)\n"))
                tool = lib.node_by_output(graph, TOOL)
                run = self.assert_run(graph, BUILD + "result.model", SOURCE, [TOOL, SOURCE, *parsed], [tool["uid"]])
                self.assert_copy(graph, BUILD + "final.model", BUILD + "result.model",
                    [BUILD + "result.model", SOURCE, *parsed], [run["uid"]])

    def test_parsed_edge_is_preserved_when_same_source_is_also_text(self):
        graph = self.make(workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in")
            .replace("OUT_NOAUTO", "IN_NOPARSE ${ARCADIA_ROOT}/shared/pattern.h.in OUT_NOAUTO")
            + "COPY_FILE(${BINDIR}/result.model final.model)\n"))
        tool = lib.node_by_output(graph, TOOL)
        run = self.assert_run(graph, BUILD + "result.model", SOURCE, [TOOL, SOURCE, LEFT, DETAIL], [tool["uid"]])
        self.assert_copy(graph, BUILD + "final.model", BUILD + "result.model",
            [BUILD + "result.model", SOURCE, LEFT, DETAIL], [run["uid"]])

    def test_build_text_inputs_keep_registered_includes_mainoutput_and_producer(self):
        for kind in ("IN", "IN_NOPARSE", "COPY_FILE"):
            with self.subTest(kind=kind):
                body = ("RUN_PROGRAM(tools/reader IN seed.dat OUT_NOAUTO marker.dat generated.h.in "
                        "OUTPUT_INCLUDES ${ARCADIA_ROOT}/shared/pattern.h.in)\n")
                if kind == "COPY_FILE":
                    body += "COPY_FILE(${BINDIR}/generated.h.in copied.h.in)\n" + consumer()
                    direct = BUILD + "copied.h.in"
                else:
                    body += consumer("${BINDIR}/generated.h.in", kind)
                    direct = BUILD + "generated.h.in"
                files = workspace(body)
                files["vellum/seed.dat"] = "real generator input\n"
                graph = self.make(files)
                gen = lib.node_by_output(graph, BUILD + "generated.h.in")
                self.assertEqual(gen["outputs"], [BUILD + "marker.dat", BUILD + "generated.h.in"])
                self.assertEqual(set(gen["inputs"]), {TOOL, "$(S)/vellum/seed.dat"})
                expected = [TOOL, BUILD + "marker.dat", BUILD + "generated.h.in", SOURCE, LEFT, DETAIL]
                deps = [gen["uid"], lib.node_by_output(graph, TOOL)["uid"]]
                if kind == "COPY_FILE":
                    cp = self.assert_copy(graph, direct, BUILD + "generated.h.in", expected[1:], [gen["uid"]])
                    expected += [direct]
                    deps += [cp["uid"]]
                self.assert_run(graph, BUILD + "result.model", direct, expected, deps)
                self.assertFalse(any("$(S)/vellum/generated.h.in" in n["inputs"] for n in graph["graph"]))

    def test_text_does_not_claim_or_acquire_shared_source_facts_in_either_root_order(self):
        files = workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in"), module="parsed")
        files.update(workspace(copied() + consumer(), module="text", directory="right"))
        for order in (("parsed", "text"), ("text", "parsed")):
            with self.subTest(order=order):
                graph = self.make(files, *order)
                tool = lib.node_by_output(graph, TOOL)
                self.assert_run(graph, "$(B)/parsed/result.model", SOURCE,
                    [TOOL, SOURCE, LEFT, DETAIL], [tool["uid"]], module="parsed")
                cp = self.assert_copy(graph, "$(B)/text/copied.h.in", SOURCE, [SOURCE], [])
                self.assert_run(graph, "$(B)/text/result.model", "$(B)/text/copied.h.in",
                    [TOOL, "$(B)/text/copied.h.in", SOURCE], [tool["uid"], cp["uid"]], module="text")

    def test_direct_noparse_does_not_acquire_another_owners_parsed_source(self):
        files = workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in"), module="parsed")
        files.update(workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in", "IN_NOPARSE"), module="text", directory="right"))
        for order in (("parsed", "text"), ("text", "parsed")):
            with self.subTest(order=order):
                graph = self.make(files, *order)
                tool = lib.node_by_output(graph, TOOL)
                self.assert_run(graph, "$(B)/text/result.model", SOURCE, [TOOL, SOURCE], [tool["uid"]], module="text")
                self.assert_run(graph, "$(B)/parsed/result.model", SOURCE, [TOOL, SOURCE, LEFT, DETAIL], [tool["uid"]], module="parsed")

    def test_two_parsed_owners_keep_the_first_sources_resolved_facts(self):
        files = workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in"), module="left")
        files.update(workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in"), module="right", directory="right"))
        for order, parsed in ((("left", "right"), [LEFT, DETAIL]), (("right", "left"), [RIGHT])):
            with self.subTest(order=order):
                graph = self.make(files, *order)
                tool = lib.node_by_output(graph, TOOL)
                for module in order:
                    self.assert_run(graph, f"$(B)/{module}/result.model", SOURCE,
                        [TOOL, SOURCE, *parsed], [tool["uid"]], module=module)

    def test_noresolve_source_views_keep_established_shared_parsed_facts(self):
        files = workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in"), module="parsed")
        files.update(workspace(consumer("${ARCADIA_ROOT}/shared/pattern.h.in"),
                               module="unresolved", directory="right", resolve=False))
        for order in (("parsed", "unresolved"), ("unresolved", "parsed")):
            with self.subTest(order=order):
                graph = self.make(files, *order)
                tool = lib.node_by_output(graph, TOOL)
                for module in order:
                    self.assert_run(graph, f"$(B)/{module}/result.model", SOURCE,
                        [TOOL, SOURCE, LEFT, DETAIL], [tool["uid"]], module=module)

    def test_text_template_include_loop_shares_obligations_without_lexical_parsing(self):
        files = workspace("ADDINCL(${BINDIR})\n" + copied().replace("copied.h.in)", "copied.h.in OUTPUT_INCLUDES other_copy.h.in)")
            + "COPY_FILE(other.h.in other_copy.h.in OUTPUT_INCLUDES copied.h.in)\n" + consumer())
        files["vellum/other.h.in"] = '#include "choice.h"\n'
        graph = self.make(files)
        originals = [SOURCE, "$(S)/vellum/other.h.in"]
        copies = [self.assert_copy(graph, BUILD + out, source, originals, [])
                  for out, source in (("copied.h.in", SOURCE), ("other_copy.h.in", originals[1]))]
        tool = lib.node_by_output(graph, TOOL)
        self.assert_run(graph, BUILD + "result.model", BUILD + "copied.h.in",
            [TOOL, BUILD + "copied.h.in", BUILD + "other_copy.h.in", *originals],
            [tool["uid"], *[cp["uid"] for cp in copies]])

    def test_mixed_loop_shares_real_parsed_views_and_preserves_text_edges(self):
        files = workspace("ADDINCL(${BINDIR})\n" + copied().replace(
            "copied.h.in)", "copied.h.in OUTPUT_INCLUDES other_copy.h.in)")
            + "COPY_FILE_WITH_CONTEXT(other.h.in other_copy.h.in OUTPUT_INCLUDES copied.h.in)\n" + consumer())
        files["shared/pattern.h.in"] = '#include "opaque-only.h"\n'
        files["includes/left/opaque-only.h"] = "// real but opaque\n"
        files["vellum/other.h.in"] = '#include "choice.h"\n'
        graph = self.make(files)
        originals = [SOURCE, "$(S)/vellum/other.h.in"]
        copies = [self.assert_copy(graph, BUILD + out, source, [*originals, LEFT, DETAIL], [])
                  for out, source in (("copied.h.in", SOURCE), ("other_copy.h.in", originals[1]))]
        tool = lib.node_by_output(graph, TOOL)
        self.assert_run(graph, BUILD + "result.model", BUILD + "copied.h.in",
            [TOOL, BUILD + "copied.h.in", BUILD + "other_copy.h.in", *originals, LEFT, DETAIL],
            [tool["uid"], *[cp["uid"] for cp in copies]])

    def test_python_runner_uses_the_same_source_text_boundary(self):
        for kind, parsed in (("IN", [LEFT, DETAIL]), ("IN_NOPARSE", [])):
            with self.subTest(kind=kind):
                files = workspace(f"RUN_PYTHON3(scripts/reader.py {kind} ${{ARCADIA_ROOT}}/shared/pattern.h.in OUT_NOAUTO result.model)\n"
                    "COPY_FILE(${BINDIR}/result.model final.model)\n")
                files["scripts/reader.py"] = "# real script input\n"
                graph = self.make(files)
                run = lib.node_by_output(graph, BUILD + "result.model")
                self.assertEqual(set(run["inputs"]), {"$(S)/scripts/reader.py", SOURCE, *parsed})
                self.assertEqual(run["cmds"][0]["cmd_args"][1:], ["$(S)/scripts/reader.py"])
                self.assertEqual(run["deps"], [])
                self.assert_copy(graph, BUILD + "final.model", BUILD + "result.model",
                    [BUILD + "result.model", SOURCE, *parsed], [run["uid"]])

    def test_text_template_and_parsed_source_can_share_one_command(self):
        files = workspace(copied() + consumer().replace("OUT_NOAUTO", "IN ${ARCADIA_ROOT}/shared/explicit.h OUT_NOAUTO"))
        files["shared/explicit.h"] = '#include "choice.h"\n'
        graph = self.make(files)
        copy = self.assert_copy(graph, BUILD + "copied.h.in", SOURCE, [SOURCE], [])
        tool = lib.node_by_output(graph, TOOL)
        self.assert_run(graph, BUILD + "result.model", BUILD + "copied.h.in",
            [TOOL, BUILD + "copied.h.in", SOURCE, "$(S)/shared/explicit.h", LEFT, DETAIL],
            [tool["uid"], copy["uid"]])

    def test_copy_keeps_real_helper_inputs_without_forwarding_ordinary_scripts(self):
        files = workspace(copied() + consumer())
        files["build/scripts/fs_tools.py"] = "from process_command_files import get_args\n"
        files["build/scripts/process_command_files.py"] = "def get_args(): return []\n"
        graph = self.make(files)
        copy = self.assert_copy(graph, BUILD + "copied.h.in", SOURCE,
            [SOURCE, "$(S)/build/scripts/fs_tools.py", "$(S)/build/scripts/process_command_files.py"], [])
        tool = lib.node_by_output(graph, TOOL)
        self.assert_run(graph, BUILD + "result.model", BUILD + "copied.h.in",
            [TOOL, BUILD + "copied.h.in", SOURCE], [tool["uid"], copy["uid"]])

    def test_source_digest_is_retained_while_opaque_header_changes_do_not_affect_consumers(self):
        files = workspace(copied() + "RUN_PROGRAM(tools/reader -o ${BINDIR}/result.model "
            "IN ${BINDIR}/copied.h.in OUT_NOAUTO result.model)\n")
        files["build/scripts/fs_tools.py"] = ("import os, shutil, sys\n"
            "if sys.argv[1] == 'copy':\n"
            "    src, dst = sys.argv[2:]\n"
            "    os.makedirs(os.path.dirname(dst) or '.', exist_ok=True)\n"
            "    shutil.copyfile(src, dst)\n")
        ws = Workspace(self, files)
        ws.make("-j1", targets=("vellum",))
        before = {path.name for path in ws.uid_files()}
        ws.write("includes/left/choice.h", "// changed opaque header\n")
        ws.make("-j1", targets=("vellum",))
        self.assertEqual({path.name for path in ws.uid_files()}, before)
        ws.write("shared/pattern.h.in", '// changed actual template data\n#include "choice.h"\n')
        ws.make("-j1", targets=("vellum",))
        added = {key for path in ws.uid_files() if path.name not in before
                 for key in json.loads(path.read_text())}
        self.assertTrue({BUILD + "copied.h.in", BUILD + "result.model"} <= added, added)


if __name__ == "__main__":
    unittest.main(verbosity=2)
