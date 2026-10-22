import unittest

import lib


MODULE = "parcel"
SOURCE = "$(S)/parcel/seed.config.in"
COPIED = "$(B)/parcel/stage/copied.config.in"
PRODUCT = "$(B)/parcel/product.model"
INSTALLED = "$(B)/parcel/installed.model"


def fixture(source="seed.config.in", destination="stage/copied.config.in", keyword="IN", reverse=False):
    copy = f"COPY_FILE(${{CURDIR}}/{source} {destination})\n"
    run = (f"RUN_PROGRAM(tools/reader --payload ${{BINDIR}}/{destination} "
           f"{keyword} ${{BINDIR}}/{destination} OUT_NOAUTO product.model)\n")
    files = {
        "parcel/ya.make": "UNION()\n" + (run + copy if reverse else copy + run)
            + "COPY_FILE(${BINDIR}/product.model installed.model)\nEND()\n",
        "parcel/" + source: 'name: "renamed fixture"\nvalue: @SETTING@\n',
    }
    lib.tool_program(files, "tools/reader", "reader")
    return files


class CopiedTemplateInputsTest(unittest.TestCase):
    def make(self, files, *targets):
        return lib.make(files, *(targets or (MODULE,)), "--sandboxing")

    def assert_consumer(self, graph, source=SOURCE, copied=COPIED, forwarded=True):
        copy = lib.node_by_output(graph, copied)
        product = lib.node_by_output(graph, PRODUCT)
        installed = lib.node_by_output(graph, INSTALLED)
        tool = lib.node_by_output(graph, "$(B)/tools/reader/reader")
        self.assertEqual(copy["inputs"], [source])
        self.assertEqual(copy["cmds"][0]["cmd_args"][-2:], [source, copied])
        self.assertEqual(product["cmds"][0]["cmd_args"], [tool["outputs"][0], "--payload", copied])
        self.assertIn(copy["uid"], product["deps"])
        self.assertIn(tool["uid"], product["deps"])
        self.assertIn(tool["outputs"][0], product["inputs"])
        self.assertIn(product["uid"], installed["deps"])
        self.assertIn(PRODUCT, installed["inputs"])
        for node in (product, installed):
            self.assertIn(copied, node["inputs"])
            self.assertEqual(source in node["inputs"], forwarded)
        self.assertTrue(all(o.startswith("$(B)/") for n in graph["graph"] for o in n["outputs"]))
        return copy, product, installed

    def test_text_template_source_reaches_build_consumer_and_next_copy(self):
        files = fixture()
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "IN ${BINDIR}", "IN bundle.config.in ${BINDIR}")
        files["parcel/bundle.config.in"] = "bundle: @BUNDLE@\n"
        graph = self.make(files)
        _, product, installed = self.assert_consumer(graph)
        for node in (product, installed):
            self.assertIn("$(S)/parcel/bundle.config.in", node["inputs"])
        union = lib.node_by_output(graph, "$(B)/parcel/parcel.pkg.fake")
        for original in (SOURCE, "$(S)/parcel/bundle.config.in"):
            self.assertIn(original, union["inputs"])

    def test_noparse_does_not_remove_producer_template_obligations(self):
        self.assert_consumer(self.make(fixture(keyword="IN_NOPARSE")))

    def test_later_copy_declaration_is_ready_without_reinterpreting_the_module(self):
        self.assert_consumer(self.make(fixture(reverse=True)))

    def test_ordinary_source_is_not_forwarded_by_a_template_output_name(self):
        self.assert_consumer(self.make(fixture(source="seed.dat")), source="$(S)/parcel/seed.dat", forwarded=False)

    def test_template_input_survives_renaming_but_only_template_buildfrom_edges_forward_it(self):
        graph = self.make(fixture(destination="stage/copied.dat"))
        copied = lib.node_by_output(graph, "$(B)/parcel/stage/copied.dat")
        product = lib.node_by_output(graph, PRODUCT)
        installed = lib.node_by_output(graph, INSTALLED)
        self.assertIn(SOURCE, product["inputs"])
        self.assertIn(copied["uid"], product["deps"])
        self.assertNotIn(SOURCE, installed["inputs"])
        self.assertNotIn(copied["outputs"][0], installed["inputs"])
        self.assertIn(product["uid"], installed["deps"])

    def test_two_copies_keep_the_original_template_and_each_real_producer(self):
        files = fixture()
        files["parcel/ya.make"] = files["parcel/ya.make"].replace("RUN_PROGRAM(",
            "COPY_FILE(${BINDIR}/stage/copied.config.in staged_again.config.in)\nRUN_PROGRAM(")
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "--payload ${BINDIR}/stage/copied.config.in IN ${BINDIR}/stage/copied.config.in",
            "--payload ${BINDIR}/staged_again.config.in IN ${BINDIR}/staged_again.config.in")
        graph = self.make(files)
        first = lib.node_by_output(graph, COPIED)
        second = lib.node_by_output(graph, "$(B)/parcel/staged_again.config.in")
        product = lib.node_by_output(graph, PRODUCT)
        for node in (second, product, lib.node_by_output(graph, INSTALLED)):
            self.assertIn(SOURCE, node["inputs"])
            self.assertIn(COPIED, node["inputs"])
        self.assertIn(first["uid"], second["deps"])
        self.assertIn(second["uid"], product["deps"])

    def test_files_peer_copies_keep_declaring_source_path_and_producer(self):
        files = fixture()
        files["supply/ya.make"] = "UNION()\nFILES(layout.config.in)\nEND()\n"
        files["supply/layout.config.in"] = "layout: @VALUE@\n"
        files["parcel/ya.make"] = files["parcel/ya.make"].replace("UNION()\n", "UNION()\nPEERDIR(supply)\n")
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "${CURDIR}/seed.config.in", "${ARCADIA_BUILD_ROOT}/supply/layout.config.in")
        graph = self.make(files)
        source = "$(S)/supply/layout.config.in"
        peer = lib.node_by_output(graph, "$(B)/supply/layout.config.in")
        copied = lib.node_by_output(graph, COPIED)
        self.assertEqual(peer["inputs"], [source])
        self.assertIn(peer["uid"], copied["deps"])
        for node in (copied, lib.node_by_output(graph, PRODUCT), lib.node_by_output(graph, INSTALLED)):
            self.assertIn(source, node["inputs"])
            self.assertIn(peer["outputs"][0], node["inputs"])
        self.assertNotIn(SOURCE, lib.node_by_output(graph, PRODUCT)["inputs"])

    def test_generated_template_keeps_secondary_output_mainoutput_and_tool_edges(self):
        files = fixture()
        lib.tool_program(files, "tools/grow", "grow")
        files["parcel/seed.dat"] = "actual generator seed\n"
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "COPY_FILE(${CURDIR}/seed.config.in", "RUN_PROGRAM(tools/grow IN seed.dat OUT_NOAUTO marker.dat generated.config.in)\nCOPY_FILE(${BINDIR}/generated.config.in")
        graph = self.make(files)
        generated = lib.node_by_output(graph, "$(B)/parcel/generated.config.in")
        copied = lib.node_by_output(graph, COPIED)
        self.assertEqual(generated["outputs"], ["$(B)/parcel/marker.dat", "$(B)/parcel/generated.config.in"])
        self.assertIn("$(S)/parcel/seed.dat", generated["inputs"])
        self.assertIn("$(B)/tools/grow/grow", generated["inputs"])
        self.assertIn(lib.node_by_output(graph, "$(B)/tools/grow/grow")["uid"], generated["deps"])
        self.assertIn(generated["uid"], copied["deps"])
        for node in (copied, lib.node_by_output(graph, PRODUCT), lib.node_by_output(graph, INSTALLED)):
            for output in generated["outputs"]:
                self.assertIn(output, node["inputs"])
            self.assertNotIn("$(S)/parcel/generated.config.in", node["inputs"])
            self.assertNotIn("$(S)/parcel/seed.dat", node["inputs"])

    def test_source_namespace_is_not_replaced_by_the_build_copy(self):
        files = fixture()
        files["parcel/stage/copied.config.in"] = "independent physical source\n"
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "--payload ${BINDIR}/stage/copied.config.in IN ${BINDIR}/stage/copied.config.in",
            "--payload ${CURDIR}/stage/copied.config.in IN ${CURDIR}/stage/copied.config.in")
        graph = self.make(files)
        product = lib.node_by_output(graph, PRODUCT)
        self.assertIn("$(S)/parcel/stage/copied.config.in", product["inputs"])
        self.assertNotIn(SOURCE, product["inputs"])
        self.assertNotIn(COPIED, product["inputs"])
        self.assertNotIn(lib.node_by_output(graph, COPIED)["uid"], product["deps"])

    def test_include_loops_share_member_sources_without_inventing_input_producers(self):
        files = fixture()
        files["parcel/other.config.in"] = "other: @VALUE@\n"
        files["parcel/ya.make"] = files["parcel/ya.make"].replace("UNION()\n", "UNION()\nSET(DONT_RESOLVE_INCLUDES no)\nADDINCL(${BINDIR})\n")
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "stage/copied.config.in)", "stage/copied.config.in OUTPUT_INCLUDES other.config.in)\nCOPY_FILE(other.config.in other.config.in OUTPUT_INCLUDES stage/copied.config.in)")
        graph = self.make(files)
        for output in (COPIED, "$(B)/parcel/other.config.in", PRODUCT, INSTALLED):
            node = lib.node_by_output(graph, output)
            for original in (SOURCE, "$(S)/parcel/other.config.in"):
                self.assertIn(original, node["inputs"])
        self.assertEqual(sum(COPIED in n["outputs"] and n["kv"].get("p") == "CP" for n in graph["graph"]), 1)

    def test_text_copy_does_not_parse_ordinary_header_contents(self):
        files = fixture(source="seed.h")
        files["parcel/seed.h"] = '#include "unrelated.h"\n'
        files["parcel/unrelated.h"] = "// not a COPY_FILE dependency\n"
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "UNION()\n", "UNION()\nSET(DONT_RESOLVE_INCLUDES no)\n")
        graph = self.make(files)
        self.assert_consumer(graph, source="$(S)/parcel/seed.h", forwarded=False)
        for node in graph["graph"]:
            if node["kv"].get("p") in ("CP", "PR"):
                self.assertNotIn("$(S)/parcel/unrelated.h", node["inputs"])

    def test_noresolve_retains_template_facts_without_resolving_lexical_includes(self):
        files = fixture()
        files["parcel/seed.config.in"] = '#include "unresolved.h"\n'
        files["parcel/unresolved.h"] = "// real but suppressed\n"
        graph = self.make(files)
        self.assert_consumer(graph)
        for node in graph["graph"]:
            self.assertNotIn("$(S)/parcel/unresolved.h", node["inputs"])

    def test_copy_with_context_preserves_real_parsed_template_dependencies(self):
        files = fixture(source="seed.h.in")
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "UNION()\n", "UNION()\nSET(DONT_RESOLVE_INCLUDES no)\n").replace(
                "COPY_FILE(${CURDIR}/seed.h.in", "COPY_FILE_WITH_CONTEXT(${CURDIR}/seed.h.in")
        files["parcel/seed.h.in"] = '#include "declared.h"\n'
        files["parcel/declared.h"] = "// real parsed dependency\n"
        graph = self.make(files)
        for output in (COPIED, PRODUCT, INSTALLED):
            node = lib.node_by_output(graph, output)
            self.assertIn("$(S)/parcel/seed.h.in", node["inputs"])
            self.assertIn("$(S)/parcel/declared.h", node["inputs"])

    def test_shared_lexical_calls_and_repeated_copy_keep_first_producer_context(self):
        files = {"shared/transfer.inc": "COPY_FILE(${CURDIR}/seed.config.in ${BINDIR}/copied.config.in)\n"
                 "COPY_FILE(${CURDIR}/seed.config.in ${BINDIR}/copied.config.in)\n"
                 "RUN_PROGRAM(tools/reader IN ${BINDIR}/copied.config.in OUT_NOAUTO report.model)\n"}
        lib.tool_program(files, "tools/reader", "reader")
        for module in ("amber", "cobalt"):
            files[module + "/ya.make"] = "UNION()\nINCLUDE(${ARCADIA_ROOT}/shared/transfer.inc)\nEND()\n"
            files[module + "/seed.config.in"] = module
        graph = self.make(files, "amber", "cobalt")
        for module, other in (("amber", "cobalt"), ("cobalt", "amber")):
            copy = lib.node_by_output(graph, f"$(B)/{module}/copied.config.in")
            report = lib.node_by_output(graph, f"$(B)/{module}/report.model")
            self.assertIn(f"$(S)/{module}/seed.config.in", report["inputs"])
            self.assertNotIn(f"$(S)/{other}/seed.config.in", report["inputs"])
            self.assertIn(copy["uid"], report["deps"])
            self.assertEqual(sum(copy["outputs"][0] in n["outputs"] and n["kv"].get("p") == "CP" for n in graph["graph"]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
