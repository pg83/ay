import unittest

import lib


class SdcPackagingTests(unittest.TestCase):
    def test_all_install_kinds_use_the_module_package_name(self):
        destinations = {
            "COMMON_LIB": "common_lib/item", "INSTALL_ROOT": "item",
            "PROFILE_HOOKS": "etc/catkin/profile.d/item", "LAUNCH": "share/named/sub/item",
            "LIB": "lib/named/sub/item", "PYTHON": "lib/python3.6/dist-packages/named/sub/item",
            "TS": "share/launcher/ui/ros_type_gen/output/named/sub/item",
        }
        for kind, destination in destinations.items():
            with self.subTest(kind=kind):
                graph = lib.make({
                    "pkg/ya.make": f"SDC_UNION(named)\nSDC_INSTALL({kind} sub/item)\nEND()\n",
                    "pkg/sub/item": "content\n",
                }, "pkg")
                copy = lib.node_by_output(graph, "$(B)/sdc_install/" + destination)
                self.assertIn("$(S)/pkg/sub/item", copy["inputs"])
                self.assertIn(copy["uid"], lib.only_node_by_kind(graph, "UN")["deps"])

    def test_install_filter_skips_disabled_targets_but_keeps_package_xml(self):
        graph = lib.make({
            "pkg/ya.make": "SDC_UNION()\nSET(SDC_INSTALL_KINDS SHARE)\n"
                "SDC_INSTALL(COMMON_BIN TARGET absent NODE unused absent PROGRAM_STARTER input unused package.xml SHARE config.xml)\nEND()\n",
            "pkg/package.xml": "<package/>\n", "pkg/config.xml": "<config/>\n",
        }, "pkg")
        lib.node_by_output(graph, "$(B)/sdc_install/common_bin/package.xml")
        lib.node_by_output(graph, "$(B)/sdc_install/share/pkg/config.xml")
        self.assertFalse(any("absent" in p or p.endswith("unused") for n in graph["graph"] for p in n["outputs"]))

    def test_supernode_links_and_program_starters_are_generated(self):
        files = {
            "pkg/ya.make": "SDC_UNION()\nSET(SDC_USE_SUPERNODE yes)\n"
                "SDC_INSTALL(COMMON_BIN NODE worker app PROGRAM_STARTER source.py starter)\nEND()\n",
            "sdg/tier0/tools/generate_supernode_link.py": "",
        }
        lib.tool_program(files, "sdg/devtools/py_monobinary_builder/generate_program_starter", "starter")
        graph = lib.make(files, "pkg")
        link = lib.node_by_output(graph, "$(B)/sdc_install/common_bin/worker")
        self.assertEqual(link["cmds"][0]["cmd_args"][-2:], ["worker", "$(B)/sdc_install/common_bin/worker"])
        starter = lib.node_by_output(graph, "$(B)/sdc_install/common_bin/starter")
        self.assertEqual(starter["cmds"][0]["cmd_args"][-2:], ["source.py", "$(B)/sdc_install/common_bin/starter"])
        package = lib.only_node_by_kind(graph, "UN")
        self.assertIn(link["uid"], package["deps"])
        self.assertIn(starter["uid"], package["deps"])

    def test_sandbox_node_and_shared_library_metadata(self):
        files = {"pkg/ya.make": "SDC_UNION()\nSDC_SB_RESOURCE_INFO(42 weights UNPACK)\n"
                 "SDC_ROSNODE_INFO(app/node)\nSDC_SOLIB_INFO(https://example.invalid/library.so)\nEND()\n"}
        lib.tool_program(files, "sdg/tier0/tools/bip/writer", "meta-writer")
        graph = lib.make(files, "pkg")
        for name, expected in (
            ("weights", ["sb-resource", "--sb-id=42", "--name=weights", "--unpack=true"]),
            ("rosnode", ["rosnode", "--node-path=app/node"]),
            ("solib", ["dynamic-library", "--url=https://example.invalid/library.so"]),
        ):
            node = lib.node_by_output(graph, f"$(B)/pkg/{name}.sdcmeta")
            self.assertEqual(node["cmds"][0]["cmd_args"][1:], expected)
            self.assertIn(node["uid"], lib.only_node_by_kind(graph, "UN")["deps"])

    def test_install_layout_and_source_directory(self):
        graph = lib.make({
            "install/ya.make": """SDC_UNION(bundle)
SRCDIR(inputs)
SET(SDC_PACKAGE_NAME robot)
SDC_INSTALL(SHARE sub/config.xml COMMON_BIN bin/run ETC_ROS setup)
END()
""",
            "inputs/sub/config.xml": "<config/>",
            "inputs/bin/run": "run",
            "inputs/setup": "setup",
        }, "install")
        package = lib.only_node_by_kind(graph, "UN")
        for src, dst in (
            ("sub/config.xml", "share/robot/sub/config.xml"),
            ("bin/run", "common_bin/run"),
            ("setup", "etc/ros/setup"),
        ):
            copied = lib.node_by_output(graph, "$(B)/sdc_install/" + dst)
            self.assertIn("$(S)/inputs/" + src, copied["inputs"])
            self.assertIn(copied["uid"], package["deps"])

    def test_resource_metadata_is_a_single_producer(self):
        files = {
            "pkg/ya.make": """SDC_UNION(model)
SDC_RESOURCE_INFO(planning model https://example.invalid/model.tar UNPACK)
END()
""",
        }
        lib.tool_program(files, "sdg/tier0/tools/bip/writer", "meta-writer")
        graph = lib.make(files, "pkg")
        meta = lib.only_node_by_kind(graph, "SM")
        self.assertEqual(meta["outputs"], ["$(B)/pkg/model.sdcmeta", "$(B)/pkg/model.global.sdcmeta"])
        self.assertEqual(meta["cmds"][0]["cmd_args"][1:], [
            "resource", "--key=planning/model", "--url=https://example.invalid/model.tar",
            "--destination=model", "--unpack=true",
        ])
        self.assertEqual(meta["cmds"][0]["stdout"], "$(B)/pkg/model.sdcmeta")
        self.assertEqual(meta["cmds"][1]["cmd_args"][-3:], [
            "copy", "$(B)/pkg/model.sdcmeta", "$(B)/pkg/model.global.sdcmeta",
        ])
        self.assertIn(meta["uid"], lib.only_node_by_kind(graph, "UN")["deps"])

    def test_install_bundles_binary_and_merged_peer_metadata(self):
        files = {
            "pkg/ya.make": "SDC_UNION()\nSDC_INSTALL(COMMON_BIN TARGET app AS runner)\nEND()\n",
            "app/ya.make": f"PROGRAM(app)\n{lib.BARE_MODULE}\nPEERDIR(model)\nUSE_SDC_META()\nEND()\n",
            "model/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}\nSDC_RESOURCE_INFO(test model https://example.invalid/model)\nEND()\n",
        }
        lib.tool_program(files, "sdg/tier0/tools/bip/writer", "meta-writer")
        graph = lib.make(files, "pkg")
        linked = lib.node_by_output(graph, "$(B)/app/app")
        self.assertIn("$(B)/app/app.sdcmeta", linked["outputs"])
        self.assertIn("$(B)/model/model.global.sdcmeta", linked["inputs"])
        merge = linked["cmds"][-1]
        self.assertEqual(merge["cmd_args"][1:], [
            "$(S)/build/internal/scripts/merge_sdcmeta.py", "$(B)",
            "$(B)/app/app.sdcmeta", "$(B)/model/model.global.sdcmeta",
        ])
        meta = lib.only_node_by_kind(graph, "SM")
        self.assertIn(meta["uid"], linked["deps"])
        bundled = lib.node_by_output(graph, "$(B)/sdc_install/common_bin/runner.sdcmeta")
        self.assertIn("$(B)/app/app", bundled["inputs"])
        self.assertEqual(bundled["cmds"][0]["cmd_args"][-2:], ["$(B)/app/app.sdcmeta", "$(B)/sdc_install/common_bin/runner.sdcmeta"])
        self.assertIn(linked["uid"], bundled["deps"])


if __name__ == "__main__":
    unittest.main()
