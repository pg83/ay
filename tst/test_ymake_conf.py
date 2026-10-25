import base64
import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")

# The clang20.json the validation slices carry; ya names the tool chain root
# after this mapping, and the reference graphs show it as CLANG-3289571294.
CLANG20_FORMULA = {
    "by_platform": {
        "darwin-arm64": {"uri": "sbr:12103780175"},
        "darwin-x86_64": {"uri": "sbr:12103727425"},
        "linux-aarch64": {"uri": "sbr:12103760137"},
        "linux-x86_64": {"uri": "sbr:12103990019"},
        "win32-x86_64": {"uri": "sbr:12103686704"},
    },
}

# Records the command line and environment ay runs the script with, then
# behaves as the fake.
# A PIC transition configures the target a second time without FORCE_NO_PIC
# (ya's _build_no_pic adds it); that run, neither nopic nor host, records '.pic'.
# An unused PIC child can outlive ay. Never create directory entries after
# fixture cleanup starts: append only to the log that Tree already created.
RECORDER = (
    "import json as _json, os as _os, sys as _sys\n"
    "_pic = '' if {'FORCE_NO_PIC=yes', 'IS_CROSS_TOOLS=yes'} & set(_sys.argv) else '.pic'\n"
    "_out = _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), 'argv.log')\n"
    "_record = (_sys.argv[2] + _pic + '\\t' + _json.dumps({'argv': _sys.argv, 'env': dict(_os.environ)}) + '\\n').encode()\n"
    "_fd = _os.open(_out, _os.O_WRONLY | _os.O_APPEND)\n"
    "try:\n"
    "    if _os.write(_fd, _record) != len(_record):\n"
    "        raise RuntimeError('incomplete configuration record')\n"
    "finally:\n"
    "    _os.close(_fd)\n"
) + lib.FAKE_YMAKE_CONF


def library(body):
    return f"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n{body}END()\n"


class Tree:
    def __init__(self, test, files, ya_conf='[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'):
        tmp = tempfile.TemporaryDirectory(prefix="ay-conf-test-")
        test.addCleanup(tmp.cleanup)
        self.src = Path(tmp.name).resolve()
        lib.seed_root(self.src)
        for relative, content in {"ya.conf": ya_conf, **files}.items():
            path = self.src / relative
            if content is None:
                path.unlink()
                continue
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        self.argv_log = self.src / "build" / "argv.log"
        self.argv_log.touch()

    def make(self, *args, timeout=60, sandboxing=True):
        env = {k: v for k, v in os.environ.items() if k not in lib.TOOLCHAIN_ENV_VARS}
        env["AY_CONF_TEST_MARKER"] = "leaks"
        sandbox_flags = ["--sandboxing"] if sandboxing else []
        return subprocess.run(
            [str(lib.AY), "make", "-j0", "-G", *sandbox_flags, "--source-root", str(self.src),
             "--host-platform", "default-linux-x86_64",
             "--target-platform", "default-linux-aarch64", *args],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=timeout,
            check=False,
        )

    def graph(self, *args, sandboxing=True):
        result = self.make(*args, sandboxing=sandboxing)
        if result.returncode != 0:
            raise AssertionError(f"exit {result.returncode}\n{result.stderr}")
        return json.loads(result.stdout)

    def recorded(self, build_type):
        prefix = build_type.encode() + b"\t"
        for line in reversed(self.argv_log.read_bytes().split(b"\n")[:-1]):
            if line.startswith(prefix):
                return json.loads(line[len(prefix):])
        raise AssertionError(f"no complete configuration record for {build_type}")


def compiled(graph):
    return sorted(
        out.rsplit("/", 1)[-1]
        for node in graph["graph"] if node["kv"]["p"] == "CC"
        for out in node["outputs"]
    )


def cc_args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


def presets(argv):
    flags = argv[argv.index("--toolchain-params") + 2:]
    if flags[:1] == ["-l"]:
        flags = flags[1:]
    assert all(flag == "-D" for flag in flags[::2]), flags
    return [tuple(kv.split("=", 1)) for kv in flags[1::2]]


class ConfVariablesTest(unittest.TestCase):
    def test_printed_variables_drive_conditions_and_expansion(self):
        files = {
            "m/ya.make": library(
                "IF (CONF_SWITCH)\n    SRCS(on.cpp)\nELSE()\n    SRCS(off.cpp)\nENDIF()\n"
                "IF (ARCH_AARCH64)\n    SRCS(arm.cpp)\nENDIF()\n"
                "CFLAGS(${CONF_DEFINE})\n"
            ),
            "m/on.cpp": "", "m/off.cpp": "", "m/arm.cpp": "",
        }
        graph = Tree(self, files).graph("--sandboxing", "m")
        self.assertEqual(compiled(graph), ["arm.cpp.o", "off.cpp.o"])
        tree = Tree(self, {**files, "build/ymake_conf.py": lib.ymake_conf(
            extra="CONF_SWITCH=yes\nCONF_DEFINE=-DFROM_CONF\nARCH_AARCH64=no\n",
        )})
        graph = tree.graph("--sandboxing", "m")
        self.assertEqual(compiled(graph), ["on.cpp.o"])
        self.assertIn("-DFROM_CONF", cc_args(graph, "$(B)/m/on.cpp.o"))

    def test_appends_comments_imports_and_raw_blocks(self):
        extra = (
            "CONF_FLAGS=-DA\n"
            "CONF_FLAGS+=-DB\n"
            "CONF_FLAGS+=\n"
            "# CONF_FLAGS=-DCOMMENTED\n"
            '@import "${CONF_ROOT}/conf/elsewhere.conf"\n'
            "\n"
            "when ($CONF_SWITCH) {\n"
            "    CONF_FLAGS=-DIN_WHEN\n"
            "}\n"
            "macro _CONF_MACRO(X) {\n"
            "    .CMD=CONF_FLAGS=-DIN_MACRO ${X}\n"
            "}\n"
            "CONF_TAIL+=-DTAIL\n"
        )
        tree = Tree(self, {
            "m/ya.make": library("SRCS(a.cpp)\nCFLAGS(${CONF_FLAGS} ${CONF_TAIL})\n"),
            "m/a.cpp": "",
            "build/ymake_conf.py": lib.ymake_conf(extra=extra),
            "build/conf/elsewhere.conf": "",
        })
        args = cc_args(tree.graph("--sandboxing", "m"), "$(B)/m/a.cpp.o")
        self.assertIn("-DA", args)
        self.assertEqual(args[args.index("-DA"):args.index("-DA") + 3], ["-DA", "-DB", "-DTAIL"])
        for absent in ("-DCOMMENTED", "-DIN_WHEN", "-DIN_MACRO"):
            self.assertNotIn(absent, args)

    def test_toolchain_root_is_the_compiler_resource_mount(self):
        tree = Tree(self, {
            "m/ya.make": library("SRCS(a.cpp)\nCFLAGS(-DOBJCOPY=${OBJCOPY_TOOL} -DCXX=${CONF_CXX})\n"),
            "m/a.cpp": "",
            "build/ymake_conf.py": lib.ymake_conf(extra="CONF_CXX=$(CLANG-1907384126)/bin/clang++\n"),
            "build/ya.conf.json": lib.ya_conf_json(formula="build/platform/clang/clang20.json"),
            "build/platform/clang/clang20.json": json.dumps(CLANG20_FORMULA),
        })
        args = cc_args(tree.graph("--sandboxing", "m"), "$(B)/m/a.cpp.o")
        self.assertIn("-DOBJCOPY=$(B)/resources/CLANG20/bin/llvm-objcopy", args)
        self.assertIn("-DCXX=$(CLANG-1907384126)/bin/clang++", args)


    def test_local_distbuild_keeps_build_info_out_of_the_cache(self):
        tree = Tree(self, {
            "m/ya.make": library("CREATE_BUILDINFO_FOR(buildinfo_data.h)\nSRCS(a.cpp)\n"),
            "m/a.cpp": '#include "m/buildinfo_data.h"\n',
            "build/scripts/build_info_gen.py": "",
            "build/scripts/xargs.py": "",
            "build/scripts/yield_line.py": "",
        })
        for args, cached in (((), False), (("--dist",), True)):
            with self.subTest(args=args):
                info = lib.node_by_output(tree.graph("--sandboxing", *args, "m"), "$(B)/m/buildinfo_data.h")
                self.assertEqual(info["kv"]["p"], "BI")
                self.assertEqual("disable_cache" in info["kv"], not cached)
                self.assertEqual(info.get("cache", True), cached)


class ConfCommandLineTest(unittest.TestCase):
    def tree(self, **files):
        return Tree(self, {
            "m/ya.make": library("SRCS(a.cpp)\n"),
            "m/a.cpp": "",
            "build/ymake_conf.py": RECORDER,
            "build/ya.conf.json": lib.ya_conf_json(formula="build/platform/clang/clang20.json"),
            "build/platform/clang/clang20.json": json.dumps(CLANG20_FORMULA),
            **files,
        })

    def test_runs_the_script_as_ya_does(self):
        tree = self.tree()
        tree.graph("--sandboxing", "-DUSER_DEFINE=1", "--target-platform-flag", "TP_FLAG=x",
                   "--host-platform-flag", "HP_FLAG=y", "m")
        for build_type, platform in (("dist-debug", "AARCH64"), ("dist-release", "X86_64")):
            with self.subTest(build_type=build_type):
                recorded = tree.recorded(build_type)
                argv = recorded["argv"]
                self.assertEqual(argv[:5], [
                    str(tree.src / "build/ymake_conf.py"), str(tree.src), build_type, "no", "--toolchain-params",
                ])
                self.assertEqual(argv[6], "-l")
                params = json.loads(base64.b64decode(argv[5]))
                self.assertEqual(params["name"], "clang20")
                self.assertEqual(params["tool_var"], "CLANG-3289571294")
                self.assertEqual(params["params"]["match_root"], "CLANG-3289571294")
                self.assertEqual(params["params"]["c_compiler"], "$(CLANG-3289571294)/bin/clang")
                self.assertEqual(params["platform"]["host"], {
                    "os": "LINUX", "arch": "x86_64", "toolchain": "default", "visible_name": "clang20",
                })
                self.assertEqual(params["platform"]["target"]["arch"], platform.lower())
                self.assertEqual(recorded["env"]["Y_PYTHON_ENTRY_POINT"], ":main")
                self.assertIn("PATH", recorded["env"])
                self.assertNotIn("AY_CONF_TEST_MARKER", recorded["env"])
        target = presets(tree.recorded("dist-debug")["argv"])
        self.assertEqual(target, sorted(target))
        self.assertEqual(dict(target), {
            "DISABLE_FLAKE8_MIGRATIONS": "yes",
            "CLANG20": "yes", "FAKEID": "sandboxing", "FORCE_NO_PIC": "yes", "IS_CROSS_SANITIZE": "yes",
            "OPENSOURCE": "yes", "SANDBOXING": "yes", "TP_FLAG": "x", "USER_DEFINE": "1", "YA": "yes",
        })
        self.assertEqual(dict(presets(tree.recorded("dist-release")["argv"])), {
            "CLANG20": "yes", "CLANG_COVERAGE": "no", "CONSISTENT_DEBUG": "yes", "FAKEID": "sandboxing",
            "HP_FLAG": "y", "IS_CROSS_SANITIZE": "yes", "IS_CROSS_TOOLS": "yes", "NO_DEBUGINFO": "yes",
            "OPENSOURCE": "yes", "SANDBOXING": "yes", "TIDY": "no", "TOOL_BUILD_MODE": "yes",
            "TRAVERSE_RECURSE": "no", "YA": "yes",
        })

    def test_pic_variant_uses_target_flags_without_force_no_pic(self):
        tree = self.tree(**{"d/ya.make": "DLL(d)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n", "d/d.cpp": ""})
        tree.graph("--sandboxing", "d")
        nopic = dict(presets(tree.recorded("dist-debug")["argv"]))
        del nopic["FORCE_NO_PIC"]
        self.assertEqual(dict(presets(tree.recorded("dist-debug.pic")["argv"])), nopic)

    def test_all_configurations_start_before_any_is_awaited(self):
        barrier = (
            "import time as _time\n"
            "_deadline = _time.monotonic() + 5\n"
            "while True:\n"
            "    with open(_out, 'rb') as _log:\n"
            "        _keys = {_line.partition(b'\\t')[0] for _line in _log.read().split(b'\\n')[:-1]}\n"
            "    if {b'dist-release', b'dist-debug', b'dist-debug.pic'} <= _keys:\n"
            "        break\n"
            "    if _time.monotonic() > _deadline:\n"
            "        raise RuntimeError('host, target and PIC configurations did not start concurrently')\n"
            "    _time.sleep(0.01)\n"
        )
        tree = self.tree(**{
            "build/ymake_conf.py": RECORDER.replace("import base64", barrier + "import base64", 1),
        })
        self.assertEqual(compiled(tree.graph("--sandboxing", "m")), ["a.cpp.o"])
        self.assertEqual(len(tree.argv_log.read_bytes().split(b"\n")) - 1, 3)
        normal = dict(presets(tree.recorded("dist-debug")["argv"]))
        del normal["FORCE_NO_PIC"]
        self.assertEqual(dict(presets(tree.recorded("dist-debug.pic")["argv"])), normal)

    def test_unused_pic_failure_does_not_change_recurse_selection(self):
        tree = self.tree(**{
            "m/ya.make": library("") + "RECURSE(d)\n",
            "m/d/ya.make": "DLL(d)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n",
            "m/d/d.cpp": "",
            "build/ymake_conf.py": RECORDER.replace(
                "import base64",
                "if _pic:\n    _sys.stderr.write('unused PIC failure\\n')\n    _sys.exit(3)\nimport base64",
                1,
            ),
        })
        graph = tree.graph("--sandboxing", "m")
        self.assertFalse(any(out.startswith("$(B)/m/d/") for node in graph["graph"] for out in node["outputs"]))

    def test_consumed_pic_failure_is_reported(self):
        tree = self.tree(**{
            "d/ya.make": "DLL(d)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n",
            "d/d.cpp": "",
            "build/ymake_conf.py": RECORDER.replace(
                "import base64",
                "if _pic:\n    _sys.stderr.write('required PIC failure\\n')\n    _sys.exit(3)\nimport base64",
                1,
            ),
        })
        result = tree.make("--sandboxing", "d")
        self.assertEqual(result.returncode, 1)
        self.assertIn("required PIC failure", result.stderr)

    def test_distbuild_runs_the_script_without_local_distbuild(self):
        tree = self.tree()
        tree.graph("--sandboxing", "--dist", "m")
        for build_type in ("dist-debug", "dist-release"):
            with self.subTest(build_type=build_type):
                argv = tree.recorded(build_type)["argv"]
                self.assertNotIn("-l", argv)
                self.assertEqual(argv[6], "-D")

    def test_tests_and_build_types_reach_the_command_line(self):
        tree = self.tree()
        tree.graph("--sandboxing", "-t", "--build", "profile", "--host-build-type", "debug", "m")
        target = dict(presets(tree.recorded("dist-profile")["argv"]))
        for key in ("TESTS_REQUESTED", "TRAVERSE_RECURSE_FOR_TESTS", "TRAVERSE_DEPENDS"):
            self.assertEqual(target[key], "yes", key)
        host = dict(presets(tree.recorded("dist-debug")["argv"]))
        self.assertEqual(host["TESTS_REQUESTED"], "yes")
        self.assertNotIn("TRAVERSE_DEPENDS", host)

    def test_local_ymake_presets_come_first(self):
        tree = self.tree(**{"local.ymake": "# comment\nLOCAL_B = 2\nSANITIZER_TYPE=address\nLOCAL_A=1\nnot a pair\n"})
        tree.graph("--sandboxing", "m")
        argv = tree.recorded("dist-debug")["argv"]
        self.assertEqual(presets(argv)[:2], [("LOCAL_A", "1"), ("LOCAL_B", "2")])
        self.assertNotIn("SANITIZER_TYPE", dict(presets(argv)))

    def test_opensource_tool_chain_description_is_the_fallback(self):
        tree = self.tree(**{
            "build/ya.conf.json": None,
            "devtools/ya/opensource/ya.conf.json": lib.ya_conf_json(version="18"),
        })
        tree.graph("--sandboxing", "m")
        params = json.loads(base64.b64decode(tree.recorded("dist-debug")["argv"][5]))
        self.assertEqual(params["name"], "clang18")


class SourceDiscoveryTest(unittest.TestCase):
    def test_inactive_sources_and_peer_makefiles_are_not_opened(self):
        tree = Tree(self, {
            "m/ya.make": library(
                "SRCS(a.cpp)\nIF (OS_WINDOWS)\n    SRCS(win.cpp)\nENDIF()\n"
                "IF (OS_WINDOWS)\n    PEERDIR(winlib)\nENDIF()\n"
            ),
            "m/a.cpp": "",
            "m/win.cpp": '#include "m/win.h"\n',
            "winlib/ya.make": library("SRCS(w.cpp)\n"),
            "winlib/w.cpp": "",
        })
        for inactive in ("m/win.cpp", "winlib/ya.make"):
            path = tree.src / inactive
            path.unlink()
            os.mkfifo(path)
        result = tree.make("--sandboxing", "m", timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(compiled(json.loads(result.stdout)), ["a.cpp.o"])

    def test_graph_is_the_same_from_run_to_run(self):
        files = {
            "m/ya.make": library("SRCS(a.cpp b.cpp)\nPEERDIR(p)\n"),
            "m/a.cpp": '#include "p/x.h"\n#include "m/y.h"\n',
            "m/b.cpp": '#include "m/y.h"\n#include <p/z.h>\n',
            "m/y.h": '#include "p/x.h"\n',
            "p/ya.make": library("SRCS(p.cpp)\n"),
            "p/p.cpp": '#include "x.h"\n',
            "p/x.h": '#include "z.h"\n',
            "p/z.h": "",
        }
        tree = Tree(self, files)
        first = tree.make("--sandboxing", "m")
        self.assertEqual(first.returncode, 0, first.stderr)
        for _ in range(3):
            self.assertEqual(tree.make("--sandboxing", "m").stdout, first.stdout)


class ConfFailuresTest(unittest.TestCase):
    def failure(self, files, *args):
        result = Tree(self, {"m/ya.make": library("SRCS(a.cpp)\n"), "m/a.cpp": "", **files}).make("--sandboxing", *args, "m")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        return ANSI.sub("", result.stderr)

    def test_missing_script_is_fatal(self):
        stderr = self.failure({"build/ymake_conf.py": None})
        self.assertIn("ymake conf: build/ymake_conf.py is missing", stderr)

    def test_failing_script_reports_its_stderr(self):
        stderr = self.failure({"build/ymake_conf.py": "import sys\nsys.stderr.write('no such toolchain\\n')\nsys.exit(3)\n"})
        self.assertIn("Config was not generated due to errors in", stderr)
        self.assertIn("no such toolchain", stderr)

    def test_missing_tool_chain_description_is_fatal(self):
        stderr = self.failure({"build/ya.conf.json": None})
        self.assertIn("ymake conf: no tool chain description", stderr)

    def test_unsupported_platform_is_fatal(self):
        stderr = self.failure({}, "--target-platform", "default-linux-riscv64")
        self.assertIn("unsupported platforms host=DEFAULT-LINUX-X86_64 target=DEFAULT-LINUX-RISCV64", stderr)

    def test_missing_compiler_version_is_fatal(self):
        stderr = self.failure({"build/ymake_conf.py": "print('CLANG=yes')\n"})
        self.assertIn("printed no COMPILER_VERSION", stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
