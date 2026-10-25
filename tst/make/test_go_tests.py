import json
import unittest

from tst import lib
from tst.make.resource_dependencies import global_resource_dependencies


STD = "contrib/go/_std_1.27/src"
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
YA_CONF = (
    "use_command_file_in_testtool = true\n"
    "default_node_requirements = '{\"cpu\": 1, \"ram\": 32, \"network\": \"restricted\"}'\n"
    "[flags]\nOPENSOURCE = \"yes\"\n\n[host_platform_flags]\nOPENSOURCE = \"yes\"\n"
)


LLD_LDFLAGS = "IF (OS_LINUX)\n    LDFLAGS(-fuse-ld=lld --ld-path=${LLD_ROOT_RESOURCE_GLOBAL}/bin/ld.lld -Wl,--no-rosegment -Wl,--build-id=sha1)\nENDIF()\n"

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
        "build/cow/on/ya.make": f"LIBRARY()\n{NO_PLATFORM}SRCS(on.c)\nEND()\n",
        "build/cow/on/on.c": "int on;\n",
        "contrib/libs/linux-headers/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}DISABLE(NEED_PLATFORM_PEERDIRS)\nEND()\n"
        ),
    }
    resources_library(files, "build/external_resources/go_tools", "GO_TOOLS")
    resources_library(files, "build/external_resources/yolint", "YOLINT")
    resources_library(files, "build/platform/lld", "LLD_ROOT", extra=LLD_LDFLAGS)
    resources_library(files, "build/platform/clang", "CLANG")
    resources_library(files, "build/platform/clang/clang-format", "CLANG_FORMAT")
    resources_library(files, "build/platform/python/ymake_python3", "YMAKE_PYTHON3")
    for path in (f"{STD}/runtime", f"{STD}/runtime/cgo", f"{STD}/context", "library/go/core/buildinfo"):
        go_module(files, path)
    return files



def go_suite_resources(case, graph, consumer, files, *, link=False, python_uri=None):
    names = [("YMAKE_PYTHON3", "build/platform/python/ymake_python3"),
             ("CLANG", "build/platform/clang")]
    if link:
        names.append(("LLD_ROOT", "build/platform/lld"))
    names.extend((("GO_TOOLS", "build/external_resources/go_tools"),
                  ("YOLINT", "build/external_resources/yolint")))
    declarations = [(name, python_uri if name == "YMAKE_PYTHON3" and python_uri is not None else
                     json.loads(files[path + "/" + name.lower() + ".json"])["by_platform"]["linux-x86_64"]["uri"])
                    for name, path in names]
    return global_resource_dependencies(case, graph, consumer, declarations)

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
        self.assertEqual(vet["deps"], [context["uid"], package["uid"],
                                       *go_suite_resources(self, graph, vet, files)])
        self.assertEqual(vet["inputs"], ["$(B)/library/go/greet/greet.a.vet.txt", "$(S)/library/go/greet"])
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
        self.assertEqual(fmt["deps"], [context["uid"], *go_suite_resources(self, graph, fmt, files)])
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

    def test_style_checks_carry_the_module_test_env(self):
        files = go_world()
        go_module(files, "library/go/greet", macros="ENV(A=1)\n")
        graph = lib.make(files, "library/go/greet", *AUTOCHECK)
        for suite in ("gofmt", "govet"):
            args = test_node(graph, f"library/go/greet/{suite}")["cmds"][0]["cmd_args"]
            self.assertEqual(args_after(args, "--env"), ["A=1"], suite)

    def test_platform_resources_of_go_suites_skip_cpp_linters(self):
        files = go_world()
        go_module(files, "library/go/greet")
        graph = lib.make(files, "library/go/greet", *AUTOCHECK)
        globals_ = args_after(test_node(graph, "library/go/greet/govet")["cmds"][0]["cmd_args"], "--global-resource")
        self.assertEqual([g.split("::")[0] for g in globals_], [
            "CLANG_RESOURCE_GLOBAL", "GO_TOOLS_RESOURCE_GLOBAL", "YMAKE_PYTHON3_RESOURCE_GLOBAL", "YOLINT_RESOURCE_GLOBAL",
        ])

    def test_native_static_library_does_not_introduce_a_linker(self):
        files = go_world()
        go_module(files, "library/go/greet", macros="PEERDIR(native)\n")
        files["native/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(n.cpp)\nEND()\n"
        files["native/n.cpp"] = ""
        graph = lib.make(files, "library/go/greet", *AUTOCHECK)
        globals_ = args_after(test_node(graph, "library/go/greet/govet")["cmds"][0]["cmd_args"], "--global-resource")
        self.assertNotIn("LLD_ROOT_RESOURCE_GLOBAL", [g.split("::")[0] for g in globals_])

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
            test_node(graph, "app/govet")["inputs"], ["$(B)/app/app.vet.txt", "$(S)/app"],
        )
        graph = lib.make(files, "app")
        self.assertEqual(test_paths(graph), [])

    def test_test_sizes_filter_suites(self):
        files = go_world()
        go_module(files, "library/go/greet")
        graph = lib.make(files, "library/go/greet", *AUTOCHECK, "--test-size", "medium")
        self.assertEqual(test_paths(graph), [])


def go_test_world():
    files = go_world()
    for path in (
        f"{STD}/os", f"{STD}/testing/internal/testdeps", f"{STD}/testing",
        f"{STD}/internal/coverage/cfile", f"{STD}/runtime/coverage", "library/go/test/yatest",
    ):
        go_module(files, path)
    go_module(files, "tools/go_test_miner", module="GO_PROGRAM")
    files["contrib/libs/libeatmydata/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(e.c)\nEND()\n"
    files["contrib/libs/libeatmydata/e.c"] = "int e;\n"
    return files


def between(args, start, end):
    begin = args.index(start) + 1
    return args[begin:args.index(end, begin)]


class GoTestModuleTest(unittest.TestCase):
    def test_external_test_sources_and_embeds_use_the_tested_package_directory(self):
        files = go_test_world()
        lib.tool_program(files, "tools/generate", "generate")
        files.update({
            "lib/svc/ya.make": "GO_LIBRARY(svc)\nIF(OS_LINUX)\nALL_GO_SRCS()\n"
                "ELSE()\nSRCS(unused.go)\nENDIF()\nGO_XTEST_SRCS(external_test.go)\n"
                "GO_XTEST_EMBED_PATTERN(*.sql)\nGO_EMBED_XTEST_DIR(assets)\n"
                "RUN_PROGRAM(tools/generate OUT_NOAUTO asset.bin)\n"
                "GO_XTEST_EMBED_BINDIR(PREFIX lib/svc/gotest asset.bin FILES asset.bin)\nEND()\n",
            "lib/svc/svc.go": "package svc\n",
            "lib/svc/external_test.go": "package svc_test\n",
            "lib/svc/query.sql": "select 1;\n",
            "lib/svc/assets/data.txt": "data\n",
            "lib/svc/gotest/ya.make": "GO_TEST_FOR(lib/svc)\nEND()\n",
        })
        graph = lib.make(files, "lib/svc/gotest", *AUTOCHECK)
        binary = lib.node_by_output(graph, "$(B)/lib/svc/gotest/svc")
        args = go_tool_args(binary)
        self.assertEqual(between(args, "++srcs", "++asm-flags"), ["$(S)/lib/svc/svc.go"])
        self.assertEqual(between(args, "++xtest_srcs", "++skip-tests"), ["$(S)/lib/svc/external_test.go"])
        embeds = [args[i + 1:i + 4] for i, arg in enumerate(args) if arg == "++embed_xtest"]
        self.assertEqual(embeds, [
            ["$(S)/lib/svc", "*.sql", "$(S)/lib/svc/query.sql"],
            ["$(S)/lib/svc", "assets", "$(S)/lib/svc/assets/data.txt"],
            ["$(B)/lib/svc/gotest", "asset.bin", "$(B)/lib/svc/gotest/asset.bin"],
        ])
        for _, _, file in embeds:
            self.assertIn(file, binary["inputs"])
        generated = lib.node_by_output(graph, "$(B)/lib/svc/gotest/asset.bin")
        self.assertIn(generated["uid"], binary["deps"])

    def test_go_test_does_not_run_the_executable_sdc_metadata_hook(self):
        files = go_test_world()
        files["build/internal/scripts/merge_sdcmeta.py"] = ""
        files["lib/tests/ya.make"] = "GO_TEST()\nUSE_SDC_META()\nGO_TEST_SRCS(check_test.go)\nEND()\n"
        files["lib/tests/check_test.go"] = "package tests\n"
        graph = lib.make(files, "lib/tests", *AUTOCHECK)
        node = lib.node_by_output(graph, "$(B)/lib/tests/tests")
        self.assertFalse(any(output.endswith(".sdcmeta") for output in node["outputs"]))
        self.assertFalse(any("$(S)/build/internal/scripts/merge_sdcmeta.py" in cmd["cmd_args"] for cmd in node["cmds"]))

    def test_go_test_links_a_test_binary_and_runs_it(self):
        files = go_test_world()
        go_module(files, "lib/greet")
        files["lib/greet/tests/ya.make"] = (
            "GO_TEST()\nGO_TEST_SRCS(greet_test.go)\nSIZE(MEDIUM)\nTAG(ya:manual)\nEND()\n"
        )
        files["lib/greet/tests/greet_test.go"] = (
            'package greet_test\nimport (\n\t"testing"\n\t"a.yandex-team.ru/lib/greet"\n)\n'
        )
        graph = lib.make(files, "lib/greet/tests", *AUTOCHECK, "--test-tag", "ya:anytag")
        self.assertEqual(test_paths(graph), [
            "lib/greet/tests/gofmt", "lib/greet/tests/govet", "lib/greet/tests/tests",
        ])
        binary = lib.node_by_output(graph, "$(B)/lib/greet/tests/tests")
        self.assertEqual(binary["kv"]["p"], "GO")
        self.assertEqual(binary["outputs"], ["$(B)/lib/greet/tests/tests", "$(B)/lib/greet/tests/tests.vet.txt"])
        args = go_tool_args(binary)
        self.assertEqual(arg_after(args, "++mode"), "test")
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [])
        self.assertEqual(between(args, "++test-miner", "++test-import-path"), [
            "$(B)/tools/go_test_miner/go_test_miner",
        ])
        self.assertEqual(between(args, "++test-import-path", "++peers"), [])
        self.assertEqual(between(args, "++peers", "++non-local-peers"), [
            f"{STD}/runtime/cgo/cgo.a", f"{STD}/internal/coverage/cfile/cfile.a",
            f"{STD}/runtime/coverage/coverage.a", f"{STD}/runtime/runtime.a",
            "library/go/core/buildinfo/buildinfo.a", f"{STD}/os/os.a",
            f"{STD}/testing/internal/testdeps/testdeps.a", f"{STD}/testing/testing.a",
            "library/go/test/yatest/yatest.a", "lib/greet/greet.a",
        ])
        self.assertEqual(between(args, "++cgo-peers", "++test_srcs"), [
            "lib/greet/tests/__vcs_version__.c.o",
            "contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a",
            "build/cow/on/libbuild-cow-on.a",
        ])
        self.assertEqual(args[args.index("++test_srcs"):], [
            "++test_srcs", "$(S)/lib/greet/tests/greet_test.go", "++xtest_srcs", "++skip-tests",
            "--ya-end-command-file",
        ])
        self.assertIn("$(B)/tools/go_test_miner/go_test_miner", binary["inputs"])

        run = test_node(graph, "lib/greet/tests/tests")
        self.assertEqual(run["kv"]["p"], "TM")
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertEqual(run["deps"], [context["uid"], binary["uid"],
                                       *go_suite_resources(self, graph, run, files, link=True)])
        self.assertEqual(run["inputs"], ["$(S)/lib/greet/tests"])
        args = run["cmds"][0]["cmd_args"]
        self.assertEqual(arg_after(args, "--test-type"), "go_test")
        self.assertEqual(arg_after(args, "--test-size"), "medium")
        self.assertEqual(arg_after(args, "--timeout"), "600")
        self.assertEqual(args_after(args, "--test-related-path"), ["$(S)/lib/greet/tests"])
        self.assertEqual(args_after(args, "--tag"), ["ya:manual"])
        self.assertIn("--supports-canonization", args)
        self.assertEqual(args_after(args, "--smooth-shutdown-signals"), ["SIGUSR2"])
        self.assertEqual(args[args.index("run_go_test"):-1], [
            "run_go_test", "--binary", "$(B)/lib/greet/tests/tests",
            "--test-work-dir", "$(B)/lib/greet/tests/test-results/tests",
            "--tracefile", "$(B)/lib/greet/tests/test-results/tests/ytest.report.trace",
            "--modulo", "1", "--modulo-index", "0", "--partition-mode", "SEQUENTIAL",
            "--output-dir", "$(B)/lib/greet/tests/test-results/tests/testing_out_stuff",
            "--project-path", "lib/greet/tests", "--timeout", "600", "--verbose",
        ])
        fmt = test_node(graph, "lib/greet/tests/gofmt")
        self.assertEqual(args_after(fmt["cmds"][0]["cmd_args"], "--test-related-path"), [
            "$(S)/lib/greet/tests/greet_test.go",
        ])

    def test_go_test_for_compiles_the_tested_package(self):
        files = go_test_world()
        files["lib/svc/ya.make"] = (
            "GO_LIBRARY(svc)\nALL_GO_SRCS()\nGO_TEST_SRCS(svc_test.go)\nEND()\n"
            "RECURSE(sub)\nRECURSE_FOR_TESTS(gotest)\n"
        )
        files["lib/svc/b.go"] = "package svc\n"
        files["lib/svc/a.go"] = "package svc\n"
        files["lib/svc/svc_test.go"] = "package svc\n"
        go_module(files, "lib/svc/sub")
        files["lib/svc/gotest/ya.make"] = "GO_TEST_FOR(lib/svc)\nEND()\n"
        graph = lib.make(files, "lib/svc", *AUTOCHECK)
        self.assertEqual(test_paths(graph), [
            "lib/svc/gofmt", "lib/svc/gotest/govet", "lib/svc/gotest/svc", "lib/svc/govet",
            "lib/svc/sub/gofmt", "lib/svc/sub/govet",
        ])
        package = lib.node_by_output(graph, "$(B)/lib/svc/svc.a")
        self.assertEqual(between(go_tool_args(package), "++srcs", "++asm-flags"), [
            "$(S)/lib/svc/a.go", "$(S)/lib/svc/b.go",
        ])
        binary = lib.node_by_output(graph, "$(B)/lib/svc/gotest/svc")
        args = go_tool_args(binary)
        self.assertEqual(between(args, "++srcs", "++asm-flags"), ["$(S)/lib/svc/a.go", "$(S)/lib/svc/b.go"])
        self.assertEqual(between(args, "++test-import-path", "++peers"), ["lib/svc"])
        self.assertEqual(between(args, "++test_srcs", "++xtest_srcs"), ["$(S)/lib/svc/svc_test.go"])
        self.assertNotIn("lib/svc/svc.a", args)
        self.assertEqual(
            test_node(graph, "lib/svc/gotest/govet")["inputs"],
            ["$(B)/lib/svc/gotest/svc.vet.txt", "$(S)/lib/svc/gotest"],
        )

    def test_autocheck_programs_link_libeatmydata_without_pie(self):
        files = go_test_world()
        files["tools/gen/ya.make"] = "PROGRAM()\nSRCS(main.cpp)\nEND()\n"
        files["tools/gen/main.cpp"] = "int main(){return 0;}\n"
        files["lib/gen/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}RUN_PROGRAM(tools/gen out.cpp OUT out.cpp)\nEND()\n"
        graph = lib.make(files, "lib/gen", "--host-platform-flag", "AUTOCHECK=yes", *AUTOCHECK)
        tool = lib.node_by_output(graph, "$(B)/tools/gen/gen")
        self.assertIn("$(B)/contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a", tool["inputs"])
        link = next(cmd["cmd_args"] for cmd in tool["cmds"] if "-Wl,--gc-sections" in cmd["cmd_args"])
        self.assertIn("-fPIC", link)
        self.assertEqual(link[-1], "-Wl,-no-pie")

    def test_all_go_test_srcs_globs_the_tested_package_tests(self):
        files = go_test_world()
        files["lib/svc/ya.make"] = "GO_LIBRARY(svc)\nALL_GO_SRCS()\nALL_GO_TEST_SRCS()\nEND()\nRECURSE_FOR_TESTS(gotest)\n"
        for name in ("a.go", "b_test.go", "a_test.go"):
            files[f"lib/svc/{name}"] = "package svc\n"
        files["lib/svc/gotest/ya.make"] = "GO_TEST_FOR(lib/svc)\nEND()\n"
        graph = lib.make(files, "lib/svc", *AUTOCHECK)
        args = go_tool_args(lib.node_by_output(graph, "$(B)/lib/svc/gotest/svc"))
        self.assertEqual(between(args, "++test_srcs", "++xtest_srcs"), ["$(S)/lib/svc/a_test.go", "$(S)/lib/svc/b_test.go"])
        package = lib.node_by_output(graph, "$(B)/lib/svc/svc.a")
        self.assertEqual(between(go_tool_args(package), "++srcs", "++asm-flags"), ["$(S)/lib/svc/a.go"])

    def test_style_cpp_brings_the_clang_format_resource(self):
        files = go_world()
        files["c/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(c.cpp)\nSTYLE_CPP()\nEND()\n"
        files["c/c.cpp"] = "int c;\n"
        files["n/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(n.cpp)\nNO_LINT()\nSTYLE_CPP()\nEND()\n"
        files["n/n.cpp"] = "int n;\n"
        go_module(files, "g/withc", macros="PEERDIR(c)\n")
        go_module(files, "g/withn", macros="PEERDIR(n)\n")
        for module, expected in (("g/withc", True), ("g/withn", False)):
            graph = lib.make(files, module, *AUTOCHECK)
            args = test_node(graph, f"{module}/govet")["cmds"][0]["cmd_args"]
            globals_ = [g.split("::")[0] for g in args_after(args, "--global-resource")]
            self.assertEqual("CLANG_FORMAT_RESOURCE_GLOBAL" in globals_, expected, module)


def go_proto_world():
    files = go_world()
    files["contrib/tools/protoc/ya.make"] = f"PROGRAM()\n{NO_PLATFORM}SRCS(main.cpp)\nEND()\n"
    files["contrib/tools/protoc/main.cpp"] = "int main(){return 0;}\n"
    go_module(files, "vendor/github.com/golang/protobuf/protoc-gen-go", module="GO_PROGRAM")
    for path in (
        f"{STD}/sync", f"{STD}/reflect", f"{STD}/fmt", f"{STD}/math",
        "vendor/github.com/golang/protobuf/proto",
        "vendor/google.golang.org/protobuf/types/descriptorpb",
        "vendor/google.golang.org/protobuf/runtime/protoimpl",
        "vendor/google.golang.org/protobuf/runtime/protoiface",
        "vendor/google.golang.org/protobuf/reflect/protoreflect",
        "vendor/google.golang.org/protobuf/types/known/anypb",
        "vendor/google.golang.org/protobuf/types/known/apipb",
        "vendor/google.golang.org/protobuf/types/known/durationpb",
        "vendor/google.golang.org/protobuf/types/known/emptypb",
        "vendor/google.golang.org/protobuf/types/known/fieldmaskpb",
        "vendor/google.golang.org/protobuf/types/known/sourcecontextpb",
        "vendor/google.golang.org/protobuf/types/known/structpb",
        "vendor/google.golang.org/protobuf/types/known/timestamppb",
        "vendor/google.golang.org/protobuf/types/known/typepb",
        "vendor/google.golang.org/protobuf/types/known/wrapperspb",
        "vendor/example.com/vtproto/protohelpers",
    ):
        go_module(files, path)
    go_module(files, "vendor/example.com/vtproto/cmd/protoc-gen-go-vtproto", module="GO_PROGRAM")
    files["contrib/libs/protobuf/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(p.cpp)\nEND()\n"
    files["contrib/libs/protobuf/p.cpp"] = "int p;\n"
    files["build/scripts/go_proto_wrapper.py"] = ""
    return files


class GoCodegenTest(unittest.TestCase):
    def test_recursed_proto_go_peer_has_one_dependency_per_archive(self):
        files = go_proto_world()
        files.update({
            "root/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\nRECURSE(leaf parent)\n",
            "root/leaf/ya.make": "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nSRCS(leaf.proto)\nEND()\n",
            "root/leaf/leaf.proto": 'syntax = "proto3";\n',
            "root/parent/ya.make": "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nSRCS(parent.proto)\nPEERDIR(root/leaf)\nEND()\n",
            "root/parent/parent.proto": 'syntax = "proto3";\n',
        })
        graph = lib.make(files, "root", "-t")
        package = lib.node_by_output(graph, "$(B)/root/parent/parent.a")
        by_uid = {node["uid"]: node for node in graph["graph"]}
        outputs = [by_uid[uid]["outputs"][0] for uid in package["deps"]]
        self.assertEqual(outputs.count("$(B)/root/leaf/leaf.a"), 1)

    def test_go_peer_of_a_proto_library_builds_its_go_variant(self):
        files = go_proto_world()
        files["p/base/ya.make"] = "PROTO_LIBRARY()\nSRCS(base.proto)\nEND()\n"
        files["p/base/base.proto"] = 'syntax = "proto3";\n'
        files["p/api/ya.make"] = (
            "PROTO_LIBRARY(api_proto)\nPROTOC_FATAL_WARNINGS()\n"
            "SET_APPEND(GO_PROTO_OPTS --go-vtproto_opt=allow-empty=true)\n"
            "GO_PROTO_PLUGIN(go-vtproto _vtproto.pb.go vendor/example.com/vtproto/cmd/protoc-gen-go-vtproto "
            "DEPS vendor/example.com/vtproto/protohelpers)\n"
            "SRCS(api.proto)\nIF (GO_PROTO)\nSRCS(extra.go)\nENDIF()\nPEERDIR(p/base)\nEND()\n"
        )
        files["p/api/api.proto"] = 'syntax = "proto3";\nimport "p/base/base.proto";\n'
        files["p/api/extra.go"] = "package api\n"
        go_module(files, "g/user", macros="PEERDIR(p/api)\n")
        graph = lib.make(files, "g/user")
        pb = lib.node_by_output(graph, "$(B)/p/api/api.pb.go")
        self.assertEqual(pb["kv"], {"p": "PB", "pc": "yellow"})
        self.assertEqual(pb["outputs"], ["$(B)/p/api/api.pb.go", "$(B)/p/api/api_vtproto.pb.go"])
        self.assertEqual(pb["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(pb["cmds"][0]["cmd_args"], [
            "$(B)/resources/YMAKE_PYTHON3/bin/python3", "$(S)/build/scripts/go_proto_wrapper.py",
            "--arcadia-prefix", "a.yandex-team.ru/", "--contrib-prefix", "vendor/",
            "--namespace", "./", "--proto", "p/api/api.proto", "--", "$(B)/contrib/tools/protoc/protoc",
            "-I=./", "-I=$(S)/", "-I=$(B)", "-I=$(S)", "-I=$(S)/contrib/libs/protobuf/src",
            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src", "--fatal_warnings",
            "--plugin=protoc-gen-go=$(B)/vendor/github.com/golang/protobuf/protoc-gen-go/protoc-gen-go",
            "--go_opt=", "--go_out=$(B)/", "--go-vtproto_opt=allow-empty=true",
            "--plugin=protoc-gen-go-vtproto=$(B)/vendor/example.com/vtproto/cmd/protoc-gen-go-vtproto/protoc-gen-go-vtproto",
            "--go-vtproto_out=$(B)/",
        ])
        self.assertIn("$(S)/p/base/base.proto", pb["inputs"])
        package = lib.node_by_output(graph, "$(B)/p/api/api_proto.a")
        args = go_tool_args(package)
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [
            "$(B)/p/api/api.pb.go", "$(B)/p/api/api_vtproto.pb.go", "$(S)/p/api/extra.go",
        ])
        peers = between(args, "++peers", "--ya-end-command-file")
        self.assertIn("p/base/base.a", peers)
        self.assertIn("vendor/example.com/vtproto/protohelpers/protohelpers.a", peers)
        self.assertIn("vendor/google.golang.org/protobuf/runtime/protoimpl/protoimpl.a", peers)
        self.assertNotIn("$(B)/contrib/libs/protobuf/libcontrib-libs-protobuf.a", package["inputs"])

    def test_grpc_in_the_go_variant_peers_go_grpc_only(self):
        files = go_proto_world()
        for path in (f"{STD}/context", "vendor/google.golang.org/grpc", "vendor/google.golang.org/grpc/codes",
                     "vendor/google.golang.org/grpc/grpclog", "vendor/google.golang.org/grpc/status"):
            go_module(files, path)
        files["contrib/libs/grpc/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(g.cpp)\nEND()\n"
        files["contrib/libs/grpc/g.cpp"] = "int g;\n"
        files["p/svc/ya.make"] = "PROTO_LIBRARY()\nGRPC()\nSRCS(svc.proto)\nEND()\n"
        files["p/svc/svc.proto"] = 'syntax = "proto3";\n'
        go_module(files, "g/user", macros="PEERDIR(p/svc)\n")
        graph = lib.make(files, "g/user")
        package = lib.node_by_output(graph, "$(B)/p/svc/svc.a")
        peers = between(go_tool_args(package), "++peers", "--ya-end-command-file")
        self.assertIn("vendor/google.golang.org/grpc/grpc.a", peers)
        self.assertFalse([o for n in graph["graph"] for o in n["outputs"] if o.startswith("$(B)/contrib/libs/grpc/")])

    def test_go_proto_peer_order_follows_conf_statement_and_plugin_order(self):
        files = go_proto_world()
        for path in (f"{STD}/context", "vendor/google.golang.org/grpc", "vendor/google.golang.org/grpc/codes",
                     "vendor/google.golang.org/grpc/grpclog", "vendor/google.golang.org/grpc/status",
                     "vendor/google.golang.org/genproto/googleapis/api/annotations", "p/dep"):
            go_module(files, path)
        files["contrib/libs/googleapis-common-protos/ya.make"] = "PROTO_LIBRARY()\nEND()\n"
        files["p/svc/ya.make"] = (
            "PROTO_LIBRARY()\nUSE_COMMON_GOOGLE_APIS(api/visibility)\nPEERDIR(p/dep)\nGRPC()\nSRCS(GLOBAL svc.proto)\n"
            "USE_COMMON_GOOGLE_APIS(api/annotations)\nEND()\n"
        )
        files["p/svc/svc.proto"] = 'syntax = "proto3";\n'
        go_module(files, "g/user", macros="PEERDIR(p/svc)\n")
        graph = lib.make(files, "g/user")
        peers = between(go_tool_args(lib.node_by_output(graph, "$(B)/p/svc/svc.a")), "++peers", "--ya-end-command-file")
        self.assertEqual(peers[:3], [
            "vendor/google.golang.org/genproto/googleapis/api/annotations/annotations.a", "p/dep/dep.a", f"{STD}/context/context.a",
        ])
        self.assertEqual(peers[3:7], [
            "vendor/google.golang.org/grpc/grpc.a", "vendor/google.golang.org/grpc/codes/codes.a",
            "vendor/google.golang.org/grpc/grpclog/grpclog.a", "vendor/google.golang.org/grpc/status/status.a",
        ])
        self.assertEqual(peers[7], f"{STD}/sync/sync.a")
        self.assertIn("vendor/google.golang.org/protobuf/types/known/wrapperspb/wrapperspb.a", peers)
        self.assertNotIn("vendor/google.golang.org/genproto/googleapis/api/visibility/visibility.a", peers)

    def test_ev_sources_of_the_go_variant_are_copied_to_proto(self):
        files = go_proto_world()
        files["build/scripts/fs_tools.py"] = ""
        files["library/cpp/eventlog/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(e.cpp)\nEND()\n"
        files["library/cpp/eventlog/e.cpp"] = "int e;\n"
        files["p/ev/ya.make"] = "PROTO_LIBRARY()\nSRCS(log.ev)\nEND()\n"
        files["p/ev/log.ev"] = 'syntax = "proto3";\n'
        go_module(files, "g/user", macros="PEERDIR(p/ev)\n")
        graph = lib.make(files, "g/user")
        copy = lib.node_by_output(graph, "$(B)/p/ev/log.ev.proto")
        self.assertEqual(copy["kv"]["p"], "CP")
        self.assertEqual(copy["cmds"][0]["cmd_args"][-2:], ["$(S)/p/ev/log.ev", "$(B)/p/ev/log.ev.proto"])
        pb = lib.node_by_output(graph, "$(B)/p/ev/log.ev.pb.go")
        self.assertEqual(pb["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(arg_after(pb["cmds"][0]["cmd_args"], "--proto"), "p/ev/log.ev.proto")
        self.assertIn(copy["uid"], pb["deps"])
        self.assertIn("$(S)/p/ev/log.ev", pb["inputs"])
        package = lib.node_by_output(graph, "$(B)/p/ev/ev.a")
        self.assertEqual(between(go_tool_args(package), "++srcs", "++asm-flags"), ["$(B)/p/ev/log.ev.pb.go"])
        self.assertFalse([o for n in graph["graph"] for o in n["outputs"] if o.startswith("$(B)/library/cpp/eventlog/")])

    def test_go_protoc_lists_own_proto_includes_before_the_peer_ones(self):
        files = go_proto_world()
        files["p/base/ya.make"] = "PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL p/base)\nSRCS(base.proto)\nEND()\n"
        files["p/base/base.proto"] = 'syntax = "proto3";\n'
        files["p/api/ya.make"] = (
            "PROTO_LIBRARY()\nPROTO_NAMESPACE(p)\nPROTO_ADDINCL(yt)\nSRCS(api.proto)\n"
            "USE_COMMON_GOOGLE_APIS(api/annotations)\nPEERDIR(p/base)\nEND()\n"
        )
        files["p/api/api.proto"] = 'syntax = "proto3";\n'
        files["contrib/libs/googleapis-common-protos/ya.make"] = "PROTO_LIBRARY()\nEND()\n"
        go_module(files, "vendor/google.golang.org/genproto/googleapis/api/annotations")
        go_module(files, "g/user", macros="PEERDIR(p/api)\n")
        graph = lib.make(files, "g/user")
        args = lib.node_by_output(graph, "$(B)/p/api/api.pb.go")["cmds"][0]["cmd_args"]
        self.assertEqual([a for a in args if a.startswith("-I=")], [
            "-I=./p", "-I=$(S)/p", "-I=$(B)", "-I=$(S)",
            "-I=$(S)/contrib/libs/googleapis-common-protos", "-I=$(S)/p", "-I=$(S)/yt",
            "-I=$(S)/contrib/libs/protobuf/src",
            "-I=$(S)/p/base",
            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
        ])

    def test_grpc_gateway_runs_protoc_with_the_gateway_plugins(self):
        files = go_proto_world()
        gateway = "vendor/github.com/grpc-ecosystem/grpc-gateway"
        for path in (f"{gateway}/v2/protoc-gen-grpc-gateway", f"{gateway}/v2/protoc-gen-openapiv2",
                     f"{gateway}/protoc-gen-grpc-gateway", f"{gateway}/protoc-gen-swagger"):
            go_module(files, path, module="GO_PROGRAM")
        for path in (f"{STD}/errors", f"{STD}/io", f"{STD}/net/http", "vendor/github.com/golang/protobuf/descriptor",
                     f"{gateway}/v2/runtime", f"{gateway}/v2/utilities", f"{gateway}/v2/protoc-gen-openapiv2/options",
                     f"{gateway}/runtime", f"{gateway}/utilities", f"{gateway}/protoc-gen-swagger/options",
                     "vendor/google.golang.org/grpc/metadata", "vendor/google.golang.org/protobuf/proto"):
            go_module(files, path)
        go_module(files, "library/go/core/resource")
        go_module(files, "library/go/core/resource/cc", module="GO_PROGRAM")
        files["p/v2/ya.make"] = (
            "PROTO_LIBRARY()\nSRCS(svc.proto)\nGO_GRPC_GATEWAY_V2_OPENAPI_SRCS(NO_JSON_NAMES_FOR_FIELDS svc.proto)\n"
            "RESOURCE(p/v2/svc.proto svc)\nEND()\n"
        )
        files["p/v2/svc.proto"] = 'syntax = "proto3";\n'
        files["p/v1/ya.make"] = "PROTO_LIBRARY()\nSRCS(svc.proto)\nGO_GRPC_GATEWAY_SWAGGER_SRCS(svc.proto)\nEND()\n"
        files["p/v1/svc.proto"] = 'syntax = "proto3";\n'
        go_module(files, "g/user", macros="PEERDIR(p/v1 p/v2)\n")
        graph = lib.make(files, "g/user")
        v2 = lib.node_by_output(graph, "$(B)/p/v2/svc.pb.gw.go")
        self.assertEqual(v2["kv"], {"p": "PB", "pc": "yellow"})
        self.assertEqual(v2["outputs"], ["$(B)/p/v2/svc.swagger.json", "$(B)/p/v2/svc.pb.gw.go"])
        args = v2["cmds"][0]["cmd_args"]
        last_include = max(i for i, a in enumerate(args) if a.startswith("-I="))
        self.assertEqual(args[last_include + 1:], [
            f"--plugin=protoc-gen-go_grpc_gw=$(B)/{gateway}/v2/protoc-gen-grpc-gateway/protoc-gen-grpc-gateway",
            "--go_grpc_gw_out=$(B)/", "--go_grpc_gw_opt=allow_repeated_fields_in_body=true",
            f"--plugin=protoc-gen-openapiv2=$(B)/{gateway}/v2/protoc-gen-openapiv2/protoc-gen-openapiv2",
            "--openapiv2_out=$(B)/", "--openapiv2_opt=json_names_for_fields=false", "--openapiv2_opt=logtostderr=true",
        ])
        v1 = lib.node_by_output(graph, "$(B)/p/v1/svc.pb.gw.go")
        self.assertEqual(v1["outputs"], ["$(B)/p/v1/svc.pb.gw.go", "$(B)/p/v1/svc.swagger.json"])
        self.assertIn("--swagger_opt=allow_repeated_fields_in_body=true", v1["cmds"][0]["cmd_args"])
        srcs = between(go_tool_args(lib.node_by_output(graph, "$(B)/p/v2/v2.a")), "++srcs", "++asm-flags")
        self.assertEqual(srcs, [
            "$(B)/p/v2/svc.pb.gw.go", "$(B)/p/v2/resource.e730f6b4e11e195b113be58f95596bb0.res.go", "$(B)/p/v2/svc.pb.go",
        ])
        peers = between(go_tool_args(lib.node_by_output(graph, "$(B)/p/v2/v2.a")), "++peers", "--ya-end-command-file")
        self.assertIn(f"{gateway}/v2/runtime/runtime.a", peers)
        self.assertIn(f"{gateway}/v2/protoc-gen-openapiv2/options/options.a", peers)
        self.assertEqual(peers[-1], "library/go/core/resource/resource.a")
        # the gateway node is a dependency; its main output (the swagger json for V2) is an input
        self.assertIn("$(B)/p/v2/svc.swagger.json", lib.node_by_output(graph, "$(B)/p/v2/v2.a")["inputs"])

    def test_mockgen_and_oapi_codegen_feed_the_package(self):
        files = go_world()
        for path in ("vendor/go.uber.org/mock/mockgen", "vendor/github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"):
            go_module(files, path, module="GO_PROGRAM")
        for path in (f"{STD}/reflect", "vendor/go.uber.org/mock/gomock"):
            go_module(files, path)
        files["go.mod"] = "module a.yandex-team.ru\n"
        files["vendor/modules.txt"] = ""
        files["build/scripts/fs_tools.py"] = ""
        go_module(files, "lib/iface")
        files["lib/iface/mocks/ya.make"] = (
            "GO_LIBRARY()\nGO_MOCKGEN_FROM(lib/iface)\nGO_MOCKGEN_EXCLUDE_INTERFACES(Internal Private)\n"
            "GO_MOCKGEN_SOURCE(iface.go IN_NOPARSE extra.txt)\n"
            "GO_OAPI_CODEGEN_V2(types api spec.yaml OUT_SUFFIX _v2 IN_NOPARSE included.yaml)\nEND()\n"
        )
        files["lib/iface/mocks/spec.yaml"] = ""
        files["lib/iface/mocks/extra.txt"] = "mock metadata\n"
        files["lib/iface/mocks/included.yaml"] = "components: {}\n"
        graph = lib.make(files, "lib/iface/mocks")
        source = lib.node_by_output(graph, "$(B)/lib/iface/mocks/iface.source.gen.go")
        self.assertEqual(source["kv"], {"p": "GS", "pc": "blue"})
        mockgen = source["cmds"][-1]
        self.assertEqual(mockgen["cmd_args"], [
            "$(B)/vendor/go.uber.org/mock/mockgen/mockgen", "-package", "mocks", "-source", "lib/iface/iface.go",
            "-exclude_interfaces=Internal,Private",
        ])
        self.assertIn("$(S)/lib/iface/mocks/extra.txt", source["inputs"])
        self.assertEqual(mockgen["cwd"], "$(B)/lib/iface/mocks/.arcadia")
        self.assertEqual(mockgen["stdout"], "$(B)/lib/iface/mocks/iface.source.gen.go")
        self.assertEqual(mockgen["env"]["GOROOT"], "$(B)/resources/GO_TOOLS")
        self.assertEqual(source["cmds"][2]["cmd_args"][2:], ["copy", "$(S)/go.mod", "$(B)/lib/iface/mocks/.arcadia/go.mod"])
        oapi = lib.node_by_output(graph, "$(B)/lib/iface/mocks/types_v2.gen.go")
        self.assertIn("$(S)/lib/iface/mocks/included.yaml", oapi["inputs"])
        self.assertEqual(oapi["kv"], {"p": "GC", "pc": "blue"})
        self.assertEqual(oapi["cmds"][0]["cmd_args"], [
            "$(B)/vendor/github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen/oapi-codegen",
            "--generate", "types", "--package", "api", "$(S)/lib/iface/mocks/spec.yaml",
        ])
        package = lib.node_by_output(graph, "$(B)/lib/iface/mocks/mocks.a")
        args = go_tool_args(package)
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [
            "$(B)/lib/iface/mocks/iface.source.gen.go", "$(B)/lib/iface/mocks/types_v2.gen.go",
        ])
        self.assertIn("vendor/go.uber.org/mock/gomock/gomock.a", between(args, "++peers", "--ya-end-command-file"))

    def test_reflection_mockgen_uses_declared_interfaces_and_package(self):
        files = go_world()
        for package in ("encoding/gob", "flag", "fmt", "os", "path", "reflect"):
            go_module(files, f"{STD}/{package}")
        for package in ("vendor/go.uber.org/mock/mockgen/model", "vendor/go.uber.org/mock/gomock", "vendor/api/iface"):
            go_module(files, package)
        for tool in ("library/go/mockgen/reflector", "vendor/go.uber.org/mock/mockgen"):
            go_module(files, tool, module="GO_PROGRAM")
        go_module(files, "g/mocks/gen", module="GO_PROGRAM", srcs="", macros=(
            "GO_MOCKGEN_CONTRIB_FROM(api/iface)\nGO_MOCKGEN_TYPES(First Second)\nGO_MOCKGEN_REFLECT()\n"
        ))
        go_module(files, "g/mocks", srcs="", macros="GO_MOCKGEN_PACKAGE(custom)\nGO_MOCKGEN_MOCKS()\n")
        graph = lib.make(files, "g/mocks")
        reflection = lib.node_by_output(graph, "$(B)/g/mocks/gen/main.go")
        self.assertEqual(reflection["cmds"][0]["cmd_args"], [
            "$(B)/library/go/mockgen/reflector/reflector", "api/iface", "First,Second",
        ])
        mock = lib.node_by_output(graph, "$(B)/g/mocks/main.go")
        self.assertEqual(mock["kv"]["p"], "GM")
        self.assertEqual(mock["cmds"][0]["cmd_args"][0], "$(B)/g/mocks/gen/gen")
        self.assertIn("custom", mock["cmds"][1]["cmd_args"])
        self.assertEqual(mock["cmds"][1]["stdout"], "$(B)/g/mocks/main.go")
        self.assertEqual(mock["cmds"][1]["env"]["GOROOT"], "$(B)/resources/GO_TOOLS")
        for output in ("$(B)/g/mocks/gen/gen", "$(B)/vendor/go.uber.org/mock/mockgen/mockgen"):
            self.assertIn(lib.node_by_output(graph, output)["uid"], mock["deps"])

    def test_go_dll_links_a_position_independent_shared_object(self):
        files = go_world()
        files["build/scripts/fs_tools.py"] = ""
        go_module(files, "lib/greet")
        files["cmd/dll/ya.make"] = "GO_DLL(greet)\nSRCS(main.go)\nPEERDIR(lib/greet)\nEND()\n"
        files["cmd/dll/main.go"] = "package main\n"
        graph = lib.make(files, "cmd/dll", "--target-platform", "default-linux-x86_64")
        dll = lib.node_by_output(graph, "$(B)/cmd/dll/libgreet.so")
        self.assertEqual(dll["kv"]["p"], "LD")
        self.assertEqual(dll["outputs"], [
            "$(B)/cmd/dll/libgreet.so", "$(B)/cmd/dll/libgreet.so.vet.txt", "$(B)/cmd/dll/libgreet.h",
        ])
        args = go_tool_args(dll)
        self.assertEqual(arg_after(args, "++mode"), "dll")
        self.assertEqual(arg_after(args, "++extldflags"), "-Wl,-soname,libgreet.so")
        self.assertIn("-shared", between(args, "++compile-flags", "++link-flags"))
        self.assertEqual(dll["cmds"][-1]["cmd_args"][1:], [
            "$(S)/build/scripts/fs_tools.py", "copy", "$(B)/cmd/dll/_cgo_export.h", "$(B)/cmd/dll/libgreet.h",
        ])
        package = lib.node_by_output(graph, "$(B)/lib/greet/greet.a")
        self.assertIn("-shared", between(go_tool_args(package), "++compile-flags", "++link-flags"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
