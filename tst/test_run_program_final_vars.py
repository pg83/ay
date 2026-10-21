import unittest

import lib


def files(native=False):
    sources = {
        "p/ya.make": "UNION()\nPEERDIR(resource)\nRUN_PROGRAM(tools/gen "
            "--resource=${LATE_RESOURCE_GLOBAL}/bin/tool --missing=${MISSING_RESOURCE_GLOBAL}/bin/tool "
            "--includes=${_C__INCLUDE} --peers=${MANAGED_PEERS} "
            "--build ${BINDIR} --source ${CURDIR} ENV LATE=${LATE_RESOURCE_GLOBAL} OUT_NOAUTO main.bin)\nEND()\n",
        "resource/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(LATE sbr:42)\nEND()\n",
    }
    if native:
        sources["p/ya.make"] = sources["p/ya.make"].replace("UNION()", "LIBRARY()\n" + lib.BARE_MODULE).replace("END()", "SRCS(main.cpp)\nEND()")
        sources["p/main.cpp"] = '#include "p/main.bin"\n'
    lib.tool_program(sources, "tools/gen", "gen")
    return sources


class RunProgramFinalVariablesTest(unittest.TestCase):
    def test_final_arguments_keep_peer_resources_and_resolve_missing_reserved_values(self):
        for native in (False, True):
            with self.subTest(native=native):
                graph = lib.make(files(native), "p")
                node = lib.node_by_output(graph, "$(B)/p/main.bin")
                self.assertEqual(node["cmds"][0]["cmd_args"], [
                    "$(B)/tools/gen/gen", "--resource=$(B)/resources/LATE/bin/tool", "--missing=/bin/tool",
                    "--includes=", "--peers=", "--build", "$(B)/p", "--source", "$(S)/p",
                ])
                self.assertEqual(node["cmds"][0]["env"]["LATE"], "$(B)/resources/LATE")

    def test_deferred_module_roots_follow_the_consuming_module(self):
        sources = {
            "gen/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/gen --build ${{BINDIR}} --source ${{CURDIR}} OUT_NOAUTO generated.h)\nEND()\n",
            "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nPEERDIR(gen)\nEND()\n",
            "app/main.cpp": '#include "gen/generated.h"\n',
        }
        lib.tool_program(sources, "tools/gen", "gen")
        for targets in (("app",), ("gen", "app"), ("app", "gen")):
            with self.subTest(targets=targets):
                graph = lib.make(sources, *targets)
                generated = lib.node_by_output(graph, "$(B)/gen/generated.h")
                self.assertEqual(generated["cmds"][0]["cmd_args"], [
                    "$(B)/tools/gen/gen", "--build", "$(B)/app", "--source", "$(S)/app",
                ])
                self.assertIn(generated["uid"], lib.node_by_output(graph, "$(B)/app/main.cpp.o")["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
