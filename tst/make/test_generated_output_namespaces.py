import hashlib
import unittest

from tst import lib
from tst.make.test_proto_cpp import proto_files


MODULE = "harbor/engine"
LIBRARY = "LIBRARY()\n" + lib.BARE_MODULE
DESTINATIONS = (
    ("${ARCADIA_ROOT}/deliveries/old/../result", "deliveries/result"),
    ("${CURDIR}/old/../result", MODULE + "/result"),
    ("${ARCADIA_BUILD_ROOT}/deliveries/old/../result", "deliveries/result"),
    ("${BINDIR}/old/../result", MODULE + "/result"),
    ("$(S)/deliveries/old/../result", "deliveries/result"),
    ("$(B)/deliveries/old/../result", "deliveries/result"),
    ("../old/../result", "harbor/result"),
)


class GeneratedOutputNamespacesTest(unittest.TestCase):
    def test_archive_outputs_use_the_same_path_for_registration_and_scheduling(self):
        for macro in ("ARCHIVE", "ARCHIVE_BY_KEYS"):
            for destination, expected in DESTINATIONS:
                with self.subTest(macro=macro, destination=destination):
                    keys = "KEYS /payload" if macro == "ARCHIVE_BY_KEYS" else ""
                    files = {
                        MODULE + "/ya.make": (
                            LIBRARY + f'{macro}(NAME "{destination}.inc" {keys} payload.txt)\n'
                            + f'COPY_FILE("$(B)/{expected}.inc" retained.h)\n'
                            + "ADDINCL(${BINDIR})\nSRCS(use.cpp)\nEND()\n"
                        ),
                        MODULE + "/payload.txt": "payload\n",
                        MODULE + "/use.cpp": '#include "retained.h"\n',
                    }
                    lib.tool_program(files, "tools/archiver", "archiver")
                    graph = lib.make(files, MODULE)
                    archive = lib.node_by_output(graph, "$(B)/" + expected + ".inc")
                    copied = lib.node_by_output(graph, "$(B)/" + MODULE + "/retained.h")
                    args = archive["cmds"][0]["cmd_args"]
                    self.assertEqual(args[args.index("-o") + 1], archive["outputs"][0])
                    self.assertIn("$(S)/" + MODULE + "/payload.txt", archive["inputs"])
                    self.assertIn(archive["uid"], copied["deps"])
                    self.assertIn(archive["outputs"][0], copied["inputs"])

    def test_join_output_expands_in_each_cached_module_context(self):
        files = {"shared/merge.inc": 'JOIN_SRCS("${CURDIR}/old/../combined.cpp" fragment.cpp)\n'}
        for module in ("harbor/copper", "harbor/indigo"):
            files[module + "/ya.make"] = LIBRARY + "INCLUDE(${ARCADIA_ROOT}/shared/merge.inc)\nEND()\n"
            files[module + "/fragment.cpp"] = "int fragment;\n"
        for targets in (("harbor/copper", "harbor/indigo"), ("harbor/indigo", "harbor/copper")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for module in targets:
                    joined = lib.node_by_output(graph, "$(B)/" + module + "/combined.cpp")
                    compiled = lib.node_by_output(graph, "$(B)/" + module + "/combined.cpp.o")
                    self.assertIn(joined["outputs"][0], joined["cmds"][0]["cmd_args"])
                    self.assertIn(joined["uid"], compiled["deps"])
                    self.assertIn("$(S)/" + module + "/fragment.cpp", compiled["inputs"])

    def test_proto_listing_and_derived_header_share_the_formatted_output_root(self):
        for destination, expected in DESTINATIONS:
            with self.subTest(destination=destination):
                files = proto_files()
                files.update({
                    MODULE + "/ya.make": (
                        "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n"
                        + f'LIST_PROTO(TO "{destination}.proto" input.proto)\nSRCS(input.proto)\nEND()\n'
                    ),
                    MODULE + "/input.proto": 'syntax = "proto3";\n',
                    "consumer/ya.make": (
                        LIBRARY + "PEERDIR(" + MODULE + ")\n"
                        + f'COPY_FILE("$(B)/{expected}.pb.h" retained.h)\n'
                        + "ADDINCL(${BINDIR})\nSRCS(use.cpp)\nEND()\n"
                    ),
                    "consumer/use.cpp": '#include "retained.h"\n',
                })
                graph = lib.make(files, "consumer")
                listing = lib.node_by_output(graph, "$(B)/" + expected + ".proto")
                header = lib.node_by_output(graph, "$(B)/" + expected + ".pb.h")
                copied = lib.node_by_output(graph, "$(B)/consumer/retained.h")
                self.assertEqual(listing["cmds"][0]["stdout"], listing["outputs"][0])
                self.assertIn("$(S)/" + MODULE + "/input.proto", listing["inputs"])
                self.assertIn(listing["uid"], header["deps"])
                self.assertIn(header["uid"], copied["deps"])

    def test_java_gentar_output_converts_source_root_without_changing_its_lexical_hash(self):
        for destination, expected in DESTINATIONS:
            with self.subTest(destination=destination):
                call = f'Main OUT_DIR "{destination}" CLASSPATH launcher'
                files = lib.java_tree(**{
                    MODULE + "/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nRUN_JAVA_PROGRAM(" + call + ")\nEND()\n",
                    "launcher/ya.make": "JAVA_PROGRAM()\nEND()\n",
                    "build/platform/java/scriptgen/ya.make": lib.resource_lib("SCRIPTGEN", 9),
                })
                graph = lib.make(files, MODULE)
                lexical = f"Main OUT_DIR {destination} CLASSPATH launcher"
                digest = hashlib.md5(lexical.encode()).hexdigest()
                archive = lib.node_by_output(graph, f"$(B)/{expected}.{digest}.gentar")
                self.assertEqual(archive["kv"]["p"], "RJ")
                self.assertIn(archive["outputs"][0], archive["cmds"][-1]["cmd_args"])
                expanded = destination.replace("${ARCADIA_ROOT}", "$(S)").replace("${CURDIR}", "$(S)/" + MODULE)
                expanded = expanded.replace("${ARCADIA_BUILD_ROOT}", "$(B)").replace("${BINDIR}", "$(B)/" + MODULE)
                self.assertIn(expanded, archive["cmds"][-1]["cmd_args"])

    def test_source_resource_inputs_do_not_bind_to_a_generated_build_candidate(self):
        for namespace in ("${ARCADIA_ROOT}/" + MODULE, "${BINDIR}"):
            with self.subTest(namespace=namespace):
                files = {
                    MODULE + "/ya.make": (
                        LIBRARY + "FROM_SANDBOX(315 OUT_NOAUTO ${CURDIR}/payload.dat)\n"
                        + f"RESOURCE({namespace}/payload.dat payload)\nEND()\n"
                    ),
                    MODULE + "/payload.dat": "physical source\n",
                    "library/cpp/resource/ya.make": LIBRARY + "END()\n",
                }
                for tool in ("tools/rescompiler", "tools/rescompressor"):
                    lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
                graph = lib.make(files, MODULE)
                packed = lib.node_by_output_prefix(graph, "$(B)/" + MODULE + "/objcopy_")
                generated = namespace == "${BINDIR}"
                root = "$(B)" if generated else "$(S)"
                self.assertIn(root + "/" + MODULE + "/payload.dat", packed["inputs"])
                self.assertEqual(any(node["kv"]["p"] == "SB" for node in graph["graph"]), generated)
                self.assertTrue(all(output.startswith("$(B)/") for node in graph["graph"] for output in node["outputs"]))

    def test_java_generated_scripts_format_outputs_and_keep_source_templates(self):
        for destination, expected in DESTINATIONS:
            with self.subTest(destination=destination):
                files = lib.java_tree(**{
                    MODULE + "/ya.make": (
                        "JAVA_PROGRAM()\n"
                        + f'GENERATE_SCRIPT(OUT "{destination}.sh" TEMPLATE ${{CURDIR}}/runner.tmpl)\nEND()\n'
                    ),
                    MODULE + "/runner.tmpl": "runner\n",
                    "build/platform/java/scriptgen/ya.make": lib.resource_lib("SCRIPTGEN", 9),
                })
                graph = lib.make(files, MODULE)
                script = lib.node_by_output(graph, "$(B)/" + expected + ".sh")
                commands = [cmd["cmd_args"] for cmd in script["cmds"]]
                self.assertTrue(any("--output" in args and args[args.index("--output") + 1] == "$(B)/" + expected + ".sh"
                                    for args in commands))
                self.assertIn("$(S)/" + MODULE + "/runner.tmpl", script["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
