import json
import unittest

import lib


STD = "contrib/go/_std_1.27/src"
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
YA_CONF = (
    "use_command_file_in_testtool = true\n"
    "default_node_requirements = '{\"cpu\": 1, \"ram\": 32, \"network\": \"restricted\"}'\n"
    "[flags]\nOPENSOURCE = \"yes\"\n\n[host_platform_flags]\nOPENSOURCE = \"yes\"\n"
)


def resources_library(files, path, name, extra=""):
    bundle = f"{name.lower()}.json"
    files[f"{path}/ya.make"] = (
        f"RESOURCES_LIBRARY()\n{extra}"
        f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON({name} {bundle})\nEND()\n"
    )
    files[f"{path}/{bundle}"] = json.dumps({"by_platform": {
        platform: {"uri": f"sbr:{len(files)}{index}"}
        for index, platform in enumerate(("linux", "linux-x86_64", "linux-aarch64"))
    }})


def go_module(files, path, body="", srcs=None, macros="", module="GO_LIBRARY"):
    name = path.rsplit("/", 1)[-1]
    files[f"{path}/ya.make"] = (
        f"{module}()\n{macros}SRCS({srcs if srcs is not None else name + '.go'})\nEND()\n"
    )
    files[f"{path}/{name}.go"] = f"package {name}\n{body}"


def go_world():
    files = {
        "ya.conf": YA_CONF,
        "build/conf/go.conf": "# tag:go-specific\nGOSTD_VERSION=1.27\nwhen ($GOSTD_VERSION == \"1.26\") {\n}\n",
        "build/scripts/append_file.py": "import sys\n",
        "build/cow/on/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\n",
        "contrib/libs/linux-headers/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}DISABLE(NEED_PLATFORM_PEERDIRS)\nEND()\n"
        ),
    }
    resources_library(files, "build/external_resources/go_tools", "GO_TOOLS")
    resources_library(files, "build/external_resources/yolint", "YOLINT")
    resources_library(files, "build/platform/lld", "LLD_ROOT")
    resources_library(files, "build/platform/clang", "CLANG")
    resources_library(files, "build/platform/clang/clang-format", "CLANG_FORMAT")
    resources_library(files, "build/platform/python/ymake_python3", "YMAKE_PYTHON3")
    for path in (f"{STD}/runtime", f"{STD}/runtime/cgo", f"{STD}/context", "library/go/core/buildinfo"):
        go_module(files, path)
    return files


def go_tool_args(node):
    for cmd in node["cmds"]:
        if "$(S)/build/scripts/go_tool.py" in cmd["cmd_args"]:
            return cmd["cmd_args"]
    raise AssertionError(f"{node['outputs']} has no go_tool.py command")


def test_node(graph, path):
    for node in graph["graph"]:
        if node["kv"].get("path") == path:
            return node
    raise AssertionError(f"graph has no test node {path!r}")


def test_paths(graph):
    return sorted(node["kv"]["path"] for node in graph["graph"] if node["kv"].get("run_test_node"))


def arg_after(args, flag):
    return args[args.index(flag) + 1]


def args_after(args, flag):
    return [args[i + 1] for i, a in enumerate(args) if a == flag]


AUTOCHECK = (
    "--target-platform", "default-linux-x86_64", "--target-platform-tests",
    "--target-platform-flag", "AUTOCHECK=yes", "-DUSE_PREBUILT_TOOLS=no",
    "--target-platform-build-type", "relwithdebinfo", "--dist",
)


class GoStdVersionTest(unittest.TestCase):
    def test_std_library_version_comes_from_go_conf(self):
        files = go_world()
        go_module(files, "library/go/greet", 'import "context"\n', macros="PEERDIR(${GOSTD}/context)\n")
        graph = lib.make(files, "library/go/greet")
        args = go_tool_args(lib.node_by_output(graph, "$(B)/library/go/greet/greet.a"))
        self.assertEqual(arg_after(args, "++std-lib-prefix"), f"{STD}/")
        self.assertEqual(arg_after(args, "++goversion"), "1.27")
        self.assertIn(f"{STD}/context/context.a", args)

    def test_define_overrides_the_conf_version(self):
        files = go_world()
        files["contrib/go/_std_1.26/src/runtime/ya.make"] = "GO_LIBRARY()\nSRCS(runtime.go)\nEND()\n"
        files["contrib/go/_std_1.26/src/runtime/runtime.go"] = "package runtime\n"
        go_module(files, "library/go/greet")
        graph = lib.make(files, "library/go/greet", "-DGOSTD_VERSION=1.26")
        args = go_tool_args(lib.node_by_output(graph, "$(B)/library/go/greet/greet.a"))
        self.assertEqual(arg_after(args, "++std-lib-prefix"), "contrib/go/_std_1.26/src/")
        self.assertEqual(arg_after(args, "++goversion"), "1.26")

    def test_autocheck_keeps_sbom_components_out_of_go_packages(self):
        files = go_world()
        files["build/internal/conf/sbom.conf"] = "\n"
        files["library/go/greet/ya.make"] = "GO_LIBRARY()\nLICENSE(MIT)\nSRCS(greet.go)\nEND()\n"
        files["library/go/greet/greet.go"] = "package greet\n"
        sbom = lib.make(files, "library/go/greet", "--target-platform", "default-linux-x86_64")
        package = lib.node_by_output(sbom, "$(B)/library/go/greet/greet.a")
        self.assertTrue(any(i.endswith(".component.sbom") for i in package["inputs"]))
        graph = lib.make(files, "library/go/greet", "--target-platform", "default-linux-x86_64", "-DAUTOCHECK=yes")
        package = lib.node_by_output(graph, "$(B)/library/go/greet/greet.a")
        self.assertFalse(any(i.endswith(".component.sbom") for i in package["inputs"]))
        self.assertFalse(any(n["kv"]["p"] == "DX" for n in graph["graph"]))


class GoStyleTestsTest(unittest.TestCase):
    def test_go_library_gets_gofmt_and_govet_suites(self):
        files = go_world()
        go_module(files, "library/go/greet", srcs="greet.go util.go")
        files["library/go/greet/util.go"] = "package greet\n"
        graph = lib.make(files, "library/go/greet", *AUTOCHECK)
        self.assertEqual(test_paths(graph), ["library/go/greet/gofmt", "library/go/greet/govet"])
        package = lib.node_by_output(graph, "$(B)/library/go/greet/greet.a")
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertEqual(context["cmds"][0]["cmd_args"][3:], [
            "{", '  "build_type": "relwithdebinfo",', '  "flags": {', '    "AUTOCHECK": "yes"', "  }", "}",
        ])
        self.assertEqual(context["requirements"], {"cpu": 1, "network": "restricted", "ram": 32})

        vet = test_node(graph, "library/go/greet/govet")
        self.assertEqual(vet["deps"], [package["uid"], context["uid"]])
        self.assertEqual(vet["inputs"], ["$(S)/library/go/greet", "$(B)/library/go/greet/greet.a.vet.txt"])
        args = vet["cmds"][0]["cmd_args"]
        self.assertEqual(args[:3], ["$(TEST_TOOL_HOST)/test_tool", "run_test", "--ya-start-command-file"])
        self.assertEqual(args[-1], "--ya-end-command-file")
        self.assertEqual(arg_after(args, "--test-type"), "govet")
        self.assertEqual(arg_after(args, "--test-ci-type"), "style")
        self.assertEqual(arg_after(args, "--timeout"), "60")
        self.assertEqual(arg_after(args, "--node-timeout"), "360")
        self.assertEqual(
            arg_after(args, "--target-platform-descriptor"),
            "default-linux-x86_64-relwithdebinfo-AUTOCHECK=yes-FAKEID=sandboxing-SANDBOXING=yes",
        )
        self.assertEqual(args_after(args, "--test-related-path"), [])
        self.assertIn("--truncate-files", args)
        self.assertEqual(args[args.index("run_check"):-1], [
            "run_check", "--source-root", "$(S)", "--checker", "run_go_vet", "--check-name", "govet",
            "--trace-path", "$(B)/library/go/greet/test-results/govet/ytest.report.trace",
            "--out-path", "$(B)/library/go/greet/test-results/govet/testing_out_stuff",
            "--log-path", "$(B)/library/go/greet/test-results/govet/testing_out_stuff/run_check.log",
            "$(B)/library/go/greet/greet.a.vet.txt", "--no-snippet-from-stdout",
        ])
        self.assertEqual(vet["kv"]["p"], "TS")
        self.assertEqual(vet["env"]["TEST_NAME"], "govet")
        self.assertNotIn("TESTING_SAVE_OUTPUT", vet["env"])

        fmt = test_node(graph, "library/go/greet/gofmt")
        self.assertEqual(fmt["deps"], [context["uid"]])
        sources = ["$(S)/library/go/greet/greet.go", "$(S)/library/go/greet/util.go"]
        self.assertEqual(fmt["inputs"], ["$(S)/library/go/greet", *sources])
        args = fmt["cmds"][0]["cmd_args"]
        self.assertEqual(args_after(args, "--test-related-path"), sources)
        self.assertEqual(args[args.index("run_go_fmt"):-1], [
            "run_go_fmt", "--gofmt", "$(B)/resources/GO_TOOLS/bin/gofmt", "--source-root", "$(S)",
            "--trace-path", "$(B)/library/go/greet/test-results/gofmt/ytest.report.trace",
            "--out-path", "$(B)/library/go/greet/test-results/gofmt/testing_out_stuff",
            *sources,
        ])

    def test_platform_resources_of_go_suites_skip_cpp_linters(self):
        files = go_world()
        go_module(files, "library/go/greet")
        graph = lib.make(files, "library/go/greet", *AUTOCHECK)
        globals_ = args_after(test_node(graph, "library/go/greet/govet")["cmds"][0]["cmd_args"], "--global-resource")
        self.assertEqual([g.split("::")[0] for g in globals_], [
            "CLANG_RESOURCE_GLOBAL", "GO_TOOLS_RESOURCE_GLOBAL", "YMAKE_PYTHON3_RESOURCE_GLOBAL", "YOLINT_RESOURCE_GLOBAL",
        ])

    def test_vendored_and_contrib_packages_have_no_style_suites(self):
        files = go_world()
        go_module(files, "vendor/github.com/pkg/errors")
        graph = lib.make(files, "vendor/github.com/pkg/errors", *AUTOCHECK)
        self.assertEqual(test_paths(graph), [])

    def test_recursed_modules_and_tests_bring_their_suites(self):
        files = go_world()
        files["app/ya.make"] = "GO_PROGRAM()\nSRCS(main.go)\nEND()\nRECURSE(lib)\nRECURSE_FOR_TESTS(extra)\n"
        files["app/main.go"] = "package main\n"
        go_module(files, "app/lib")
        go_module(files, "app/extra")
        graph = lib.make(files, "app", *AUTOCHECK)
        self.assertEqual(test_paths(graph), [
            "app/extra/gofmt", "app/extra/govet", "app/gofmt", "app/govet", "app/lib/gofmt", "app/lib/govet",
        ])
        self.assertEqual(
            test_node(graph, "app/govet")["inputs"], ["$(S)/app", "$(B)/app/app.vet.txt"],
        )
        graph = lib.make(files, "app")
        self.assertEqual(test_paths(graph), [])

    def test_test_sizes_filter_suites(self):
        files = go_world()
        go_module(files, "library/go/greet")
        graph = lib.make(files, "library/go/greet", *AUTOCHECK, "--test-size", "medium")
        self.assertEqual(test_paths(graph), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
