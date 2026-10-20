import unittest

import lib
from test_link_go_modules import fixture


def files():
    result = fixture(sbom=False)
    for peer in (
        "sdg/sdc/ros/configuration/golaunch",
        "sdg/sdc/ros/configuration/golaunch/golaunch_context",
    ):
        result[f"{peer}/ya.make"] = f"GO_LIBRARY()\n{lib.BARE_MODULE}\nSRCS(a.go)\nEND()\n"
        result[f"{peer}/a.go"] = "package support\n"
    result["sdg/sdc/launcher/myriapodalaunch/yamakes/myriapodalaunch_main/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}\nSRCS(main.cpp)\nEND()\n"
    result["sdg/sdc/launcher/myriapodalaunch/yamakes/myriapodalaunch_main/main.cpp"] = "int main(){}\n"
    result["build/internal/scripts/gen_mlaunch.py"] = ""
    result["build/internal/scripts/merge_sdcmeta.py"] = ""
    for peer in ("sdg/sdc/launcher/profiles/sdc_install", "sdg/sdc/ros/vehicles/sdc_install"):
        result[f"{peer}/ya.make"] = "SDC_UNION()\nEND()\n"
    return result


class MlaunchTests(unittest.TestCase):
    def test_resource_peers_select_nested_union_variants(self):
        sources = files()
        sources.update({
            "pipeline/ya.make": f"MLAUNCH_PIPELINE(run)\n{lib.BARE_MODULE}\nPEERDIR(mid)\nEND()\n",
            "mid/ya.make": f"MLAUNCH_LIBRARY(mid)\n{lib.BARE_MODULE}\nNO_GOLAUNCH_SRCS()\nRESOURCE_PEERDIR(leaf)\nEND()\n",
            "leaf/ya.make": f"MLAUNCH_LIBRARY(leaf)\n{lib.BARE_MODULE}\nNO_GOLAUNCH_SRCS()\nRESOURCE_PEERDIR(data)\nEND()\n",
            "data/ya.make": "UNION()\nFILES(asset.txt)\nEND()\n",
            "data/asset.txt": "asset\n",
        })
        graph = lib.make(sources, "pipeline")
        package = lib.node_by_output(graph, "$(B)/pipeline/run.final.pkg.fake")
        self.assertIn("$(B)/data/asset.txt", package["inputs"])
        self.assertIn("$(B)/pipeline/data/asset.txt", package["outputs"])

    def test_only_tags_selects_golaunch_variant(self):
        sources = files()
        sources["pipeline/ya.make"] = f"MLAUNCH_PIPELINE(run)\n{lib.BARE_MODULE}\nONLY_TAGS(GOLAUNCH)\nEND()\n"
        graph = lib.make(sources, "pipeline")
        lib.node_by_output(graph, "$(B)/pipeline/run.golaunch")
        outputs = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertNotIn("$(B)/pipeline/run", outputs)
        self.assertNotIn("$(B)/pipeline/run.final.pkg.fake", outputs)

    def test_pipeline_selects_cpp_go_and_deploy_self_peers(self):
        sources = files()
        sources.update({
            "pipeline/ya.make": f"MLAUNCH_PIPELINE(run)\n{lib.BARE_MODULE}\nPEERDIR(component)\nEND()\n",
            "component/ya.make": f"""MLAUNCH_LIBRARY(part)
{lib.BARE_MODULE}
GOLAUNCH_SRCDIR(component/launch)
GOLAUNCH_SRCS(part.go)
GOLAUNCH_FUNCTION(Start)
RUNTIME_PEERDIR(runtime)
RESOURCE_PEERDIR(data)
END()
""",
            "component/launch/part.go": "package part\n",
            "runtime/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}\nSRCS(runtime.cpp)\nEND()\n",
            "runtime/runtime.cpp": "int runtime;\n",
            "data/ya.make": "UNION()\nFILES(data.txt)\nEND()\n",
            "data/data.txt": "data\n",
        })
        graph = lib.make(sources, "pipeline")
        cpp = lib.node_by_output(graph, "$(B)/pipeline/run")
        go = lib.node_by_output(graph, "$(B)/pipeline/run.golaunch")
        lib.node_by_output(graph, "$(B)/runtime/runtime.cpp.o")
        generated = lib.node_by_output(graph, "$(B)/pipeline/launch.go")
        self.assertEqual(generated["kv"]["p"], "ML")
        args = generated["cmds"][0]["cmd_args"]
        self.assertEqual(args[1:5], ["$(S)/build/internal/scripts/gen_mlaunch.py", "--type", "golaunch", "--local-peers"])
        self.assertIn("component/part.a", args)
        self.assertEqual(args[args.index("--deps") + 1:], ["component:Start"])
        self.assertIn("$(B)/pipeline/run.golaunch.sdcmeta", go["outputs"])
        package = lib.node_by_output(graph, "$(B)/pipeline/run.final.pkg.fake")
        self.assertIn(cpp["uid"], package["deps"])
        self.assertIn(go["uid"], package["deps"])
        self.assertIn("$(B)/pipeline/run.deploy.sdcmeta", package["outputs"])
        self.assertIn("$(B)/data/data.txt", package["inputs"])

    def test_generated_component_propagates_functions_overrides_and_callbacks(self):
        sources = files()
        sources.update({
            "app/ya.make": f"GOLAUNCH_PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR(component)\nGOLAUNCH_OVERRIDE(component Other)\nEND()\n",
            "component/ya.make": f"MLAUNCH_LIBRARY(part)\n{lib.BARE_MODULE}\nNO_GOLAUNCH_SRCS()\nGENERATE_GOLAUNCH_CALLBACKS(robot)\nEND()\n",
        })
        graph = lib.make(sources, "app")
        generated = lib.node_by_output(graph, "$(B)/component/launch.go")
        args = generated["cmds"][0]["cmd_args"]
        self.assertEqual(args[2:4], ["--type", "component"])
        self.assertEqual(args[-2:], ["--gen-callbacks", "robot"])
        app = lib.node_by_output(graph, "$(B)/app/launch.go")
        self.assertEqual(app["cmds"][0]["cmd_args"][-4:], ["--deps", "component:Launch", "--deps-override", "component:Other"])

    def test_golaunch_composite_links_generated_main_over_component_go_variants(self):
        sources = files()
        std = "contrib/go/_std_1.26/src"
        for peer in (f"{std}/fmt", f"{std}/sort"):
            sources[f"{peer}/ya.make"] = f"GO_LIBRARY()\n{lib.BARE_MODULE}\nSRCS(a.go)\nEND()\n"
            sources[f"{peer}/a.go"] = "package std\n"
        for name in ("first", "second"):
            sources[f"{name}/ya.make"] = f"MLAUNCH_LIBRARY({name})\n{lib.BARE_MODULE}\nGOLAUNCH_SRCS({name}.go)\nGOLAUNCH_FUNCTION(Run)\nEND()\n"
            sources[f"{name}/{name}.go"] = f"package {name}\n"
        sources["build/internal/scripts/gen_composite_golaunch.py"] = ""
        sources["composite/ya.make"] = f"GOLAUNCH_COMPOSITE(mono)\n{lib.BARE_MODULE}\nGOLAUNCH_COMPOSITE_COMPONENTS(\n    first\n    second\n)\nEND()\n"
        graph = lib.make(sources, "composite")
        main = lib.node_by_output(graph, "$(B)/composite/main.go")
        self.assertEqual(main["kv"], {"p": "GL", "pc": "cyan"})
        self.assertEqual(main["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/internal/scripts/gen_composite_golaunch.py", "--local-peers", "first", "second",
        ])
        self.assertEqual(main["inputs"], ["$(S)/build/internal/scripts/gen_composite_golaunch.py"])
        link = next(c["cmd_args"] for c in lib.node_by_output(graph, "$(B)/composite/mono")["cmds"] if "++srcs" in c["cmd_args"])
        self.assertEqual(link[link.index("++srcs") + 1], "$(B)/composite/main.go")
        peers = link[link.index("++peers") + 1:link.index("++non-local-peers")]
        for archive in (f"{std}/fmt/fmt.a", f"{std}/sort/sort.a", "sdg/sdc/ros/configuration/golaunch/golaunch.a", "first/first.a", "second/second.a"):
            self.assertIn(archive, peers)

    def test_go_variant_of_mlaunch_component_builds_its_golaunch_sources(self):
        sources = files()
        sources["comp/ya.make"] = f"""MLAUNCH_COMPONENT(part)
{lib.BARE_MODULE}
GOLAUNCH_SRCDIR(comp/launch)
GOLAUNCH_FUNCTION(Start)
GOLAUNCH_SRCS(part.go)
RESOURCE_PEERDIR(data)
NODE_BUILDER(
    ns::Build
    NAME part
    TARGET comp/builder
    HEADER comp/builder.h
)
END()
"""
        sources["comp/launch/part.go"] = "package part\n"
        sources["app/ya.make"] = f"GOLAUNCH_PROGRAM(app)\n{lib.BARE_MODULE}\nPEERDIR(comp)\nEND()\n"
        graph = lib.make(sources, "app")
        package = lib.node_by_output(graph, "$(B)/comp/part.a")
        args = next(c["cmd_args"] for c in package["cmds"] if "++srcs" in c["cmd_args"])
        self.assertEqual(args[args.index("++srcs") + 1:args.index("++asm-flags")], ["$(S)/comp/launch/part.go"])
        self.assertIn("sdg/sdc/ros/configuration/golaunch/golaunch_context/golaunch_context.a", args)
        generated = lib.node_by_output(graph, "$(B)/app/launch.go")["cmds"][0]["cmd_args"]
        self.assertEqual(generated[generated.index("--deps") + 1:], ["comp:Start"])


if __name__ == "__main__":
    unittest.main()
