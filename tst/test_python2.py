import base64
import hashlib
import unittest

import lib


STUB_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(s.cpp)\nEND()\n"
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"

PEER_STUBS = (
    "build/rules/py2_deprecation",
    "contrib/deprecated/python/subprocess32",
    "contrib/tools/python/lib",
    "contrib/tools/python/src/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime",
    "library/python/runtime/main",
    "library/python/runtime_py3",
    "library/python/testing/import_test",
)

TOOLS = (
    "tools/archiver",
    "tools/py2cc",
    "tools/py2cc/stage0pycc",
    "tools/rescompiler",
    "tools/rescompressor",
)

CONTRIB_LIBS_PYTHON = (
    "PY23_LIBRARY()\n"
    "NO_PYTHON_INCLUDES()\n"
    "IF (MODULE_TAG == \"PY2\")\n"
    "    CFLAGS(GLOBAL -DUSE_PYTHON2)\n"
    "    PEERDIR(contrib/tools/python/lib library/python/runtime)\n"
    "ELSE()\n"
    "    CFLAGS(GLOBAL -DUSE_PYTHON3)\n"
    "    PEERDIR(library/python/runtime_py3)\n"
    "ENDIF()\n"
    "END()\n"
)

STAGE0 = "$(B)/tools/py2cc/stage0pycc/py2cc"

CYTHON_UTILITY = (
    "arrayarray.h", "AsyncGen.c", "Buffer.c", "Builtins.c", "CConvert.pyx", "CMath.c",
    "CommonStructures.c", "CommonTypes.c", "Complex.c", "Coroutine.c", "CpdefEnums.pyx",
    "CppConvert.pyx", "CppSupport.cpp", "CythonFunction.c", "Dataclasses.c", "Embed.c",
    "Exceptions.c", "ExtensionTypes.c", "FunctionArguments.c", "ImportExport.c",
    "MemoryView.pyx", "MemoryView_C.c", "ModuleSetupCode.c", "NumpyImportArray.c",
    "ObjectHandling.c", "Optimize.c", "Overflow.c", "Printing.c", "Profile.c",
    "StringTools.c", "TestCyUtilityLoader.pyx", "TestCythonScope.pyx",
    "TestUtilityLoader.c", "UFuncs_C.c",
)

CYTHON_HEADERS = (
    "contrib/tools/cython_py2/generated_c_headers.h",
    "contrib/tools/cython_py2/generated_cpp_headers.h",
    "contrib/libs/python/Include/compile.h",
    "contrib/libs/python/Include/frameobject.h",
    "contrib/libs/python/Include/longintrepr.h",
    "contrib/libs/python/Include/pyconfig.h",
    "contrib/libs/python/Include/Python.h",
    "contrib/libs/python/Include/pythread.h",
    "contrib/libs/python/Include/structmember.h",
    "contrib/libs/python/Include/traceback.h",
    "contrib/libs/cxxsupp/openmp/omp.h",
)


def python2_base():
    files = {f"{path}/ya.make": STUB_LIBRARY for path in PEER_STUBS}
    files.update({f"{path}/s.cpp": "int s;\n" for path in PEER_STUBS})
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1] if path != "tools/py2cc/stage0pycc" else "py2cc")
    files["contrib/libs/python/ya.make"] = CONTRIB_LIBS_PYTHON
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


def section(node, flag):
    out = []
    rest = args(node)[args(node).index(flag) + 1:]
    for arg in rest:
        if arg.startswith("--"):
            break
        out.append(arg)
    return out


def keys(node):
    return [base64.b64decode(key).decode() for key in section(node, "--keys")]


def objcopy_nodes(graph, module):
    prefix = f"$(B)/{module}/objcopy_"
    return [
        node for node in graph["graph"]
        if any(out.startswith(prefix) for out in node["outputs"])
    ]


def objcopy_with_kv(graph, module, kv):
    nodes = [node for node in objcopy_nodes(graph, module) if kv in section(node, "--kvs")]
    if len(nodes) != 1:
        raise AssertionError(f"expected one objcopy with {kv!r}, got {len(nodes)}")
    return nodes[0]


def py2_resource_hash(module, items):
    parts = [f"$S/{module}"]
    for path, mod in items:
        parts += [
            path,
            base64.b64encode(f"/py_modules/{mod}".encode()).decode(),
            f'resfs/src//py_modules/{mod}=${{rootrel;context=TEXT;input=TEXT:"{path}"}}',
            f"{path}.yapyc",
            base64.b64encode(f"/py_code/{mod}".encode()).decode(),
        ]
    return hashlib.md5((",".join(sorted(parts)) + "PY2").encode()).hexdigest()[:26]


class Python2Test(unittest.TestCase):
    def test_py2_lint_uses_resolved_source_files_and_the_linter_resource(self):
        files = python2_base()
        files.update({
            "prog/ya.make": (
                f"PY2_PROGRAM()\n{NO_PLATFORM}NO_PYTHON_INCLUDES()\nNO_CHECK_IMPORTS()\n"
                "SRCDIR(elsewhere)\nCOPY_FILE(a.py copied.py)\n"
                "PY_SRCS(a.py copied.py)\nPY_SRCS(b.py)\nEND()\n"
            ),
            "prog/a.py": "",
            "elsewhere/b.py": "",
        })
        graph = lib.make(files, "prog", "-t")
        run = lib.node_by_output(graph, "$(B)/prog/test-results/py2_flake8/meta.json")
        command = args(run)
        self.assertEqual(run["kv"]["p"], "TS")
        self.assertIn("FLAKE8_PY2_RESOURCE_GLOBAL::$(B)/resources/FLAKE8_PY2", command)
        self.assertEqual(section(run, "--extra-param"), [
            "DISABLE_FLAKE8_MIGRATIONS=yes", "$(S)/elsewhere/b.py", "$(S)/prog/a.py",
        ])
        self.assertEqual(run["inputs"], [
            "$(S)/build/config/tests/flake8/flake8.conf", "$(S)/elsewhere/b.py",
            "$(S)/prog", "$(S)/prog/a.py", "$(S)/tools/flake8_linter/__main__.py",
        ])
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertEqual(run["deps"], [context["uid"]])

    def test_py2_lint_obeys_disable_flags(self):
        for body, flags in (("NO_LINT()\n", []), ("", ["-DDISABLE_FLAKE8=yes"]), ("", ["-DCPP_ANALYSIS_MODE=yes"])):
            with self.subTest(body=body, flags=flags):
                files = python2_base()
                files.update({
                    "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}{body}PY_SRCS(a.py)\nEND()\n",
                    "lib/a.py": "",
                })
                graph = lib.make(files, "lib", "-t", *flags)
                self.assertFalse(any("py2_flake8" in out for n in graph["graph"] for out in n["outputs"]))

    def test_py2_lint_config_and_processing_time_control_chunks(self):
        files = python2_base()
        files.update({
            "ya.conf": 'disable_flake8_migrations = false\n[flags]\nOPENSOURCE = "yes"\n',
            "lib/ya.make": (
                f"PY2_LIBRARY()\n{NO_PLATFORM}SET(PYTHON_LINTERS_DEFAULT_CONFIGS lint/defaults.json)\n"
                "SET(FLAKE8_FILE_PROCESSING_TIME 31)\nPY_SRCS(a.py b.py c.py)\nEND()\n"
            ),
            "lint/defaults.json": '{"py2_flake8":"lint/custom.conf"}',
            "lib/a.py": "",
            "lib/b.py": "",
            "lib/c.py": "",
        })
        graph = lib.make(files, "lib", "-t")
        runs = [n for n in graph["graph"] if n["kv"].get("run_test_node")]
        self.assertEqual(len(runs), 2)
        lint_files = []
        for run in runs:
            command = args(run)
            related = [command[i + 1] for i, arg in enumerate(command) if arg == "--test-related-path"]
            self.assertEqual(related[-3:], [
                "$(S)/lint/custom.conf", "$(S)/build/rules/flake8/migrations.yaml",
                "$(S)/tools/flake8_linter/__main__.py",
            ])
            self.assertIn("$(S)/lint/custom.conf", command)
            self.assertIn("$(S)/build/rules/flake8/migrations.yaml", command)
            self.assertNotIn("--extra-param", command)
            lint_files.append([a for a in command[command.index("run_custom_lint"):] if a in ("$(S)/lib/a.py", "$(S)/lib/b.py", "$(S)/lib/c.py")])
        self.assertEqual(sorted(lint_files), [["$(S)/lib/a.py", "$(S)/lib/c.py"], ["$(S)/lib/b.py"]])

    def test_py23_python2_variant_inherits_lint_but_native_variant_does_not(self):
        for macro, sources, has_linter in (("PY23_LIBRARY", "PY_SRCS(a.py)", True), ("PY23_NATIVE_LIBRARY", "SRCS(a.cpp)", False)):
            with self.subTest(macro=macro):
                files = python2_base()
                files["contrib/libs/python/ya.make"] = CONTRIB_LIBS_PYTHON.replace("END()", "NO_LINT()\nEND()")
                files.update({
                    "lib/ya.make": f"{macro}()\n{NO_PLATFORM}{sources}\nEND()\n",
                    "lib/a.py": "",
                    "lib/a.cpp": "int a;\n",
                    "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}USE_PYTHON2()\nPEERDIR(lib)\nSRCS(m.cpp)\nEND()\n",
                    "prog/m.cpp": "int main(){return 0;}\n",
                })
                graph = lib.make(files, "prog", "-t")
                outputs = {out for n in graph["graph"] for out in n["outputs"]}
                self.assertEqual("$(B)/resources/FLAKE8_PY2" in outputs, has_linter)
                self.assertNotIn("$(B)/lib/test-results/py2_flake8/meta.json", outputs)

    def test_py2_library_compiles_sources_with_py2cc_into_resources(self):
        files = python2_base()
        files.update({
            "lib/ya.make": (
                "PY2_LIBRARY()\n"
                "ENABLE(USE_LIGHT_PY2CC)\n"
                "PY_SRCS(TOP_LEVEL top.py)\n"
                "PY_SRCS(pkg/mod.py __init__.py)\n"
                "SRCS(x.cpp)\n"
                "END()\n"
            ),
            "lib/top.py": "",
            "lib/pkg/mod.py": "",
            "lib/__init__.py": "",
            "lib/x.cpp": "int x;\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")

        top = lib.node_by_output(graph, "$(B)/lib/top.py.yapyc")
        self.assertEqual(top["kv"]["p"], "PY")
        self.assertEqual(args(top), [STAGE0, "lib/top.py-", "$(S)/lib/top.py", "$(B)/lib/top.py.yapyc"])
        self.assertEqual(top["inputs"], [STAGE0, "$(S)/lib/top.py"])
        self.assertEqual(top["cmds"][0]["env"]["PYTHONHASHSEED"], "0")
        nested = lib.node_by_output(graph, "$(B)/lib/pkg/mod.py.zsw2.yapyc")
        self.assertEqual(args(nested)[1:3], ["lib/pkg/mod.py-", "$(S)/lib/pkg/mod.py"])

        top_res = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/top=lib/top.py")
        self.assertEqual(keys(top_res), ["/py_modules/top", "/py_code/top"])
        self.assertEqual(section(top_res, "--inputs"), ["$(S)/lib/top.py", "$(B)/lib/top.py.yapyc"])
        self.assertEqual(
            top_res["outputs"],
            [f"$(B)/lib/objcopy_{py2_resource_hash('lib', [('top.py', 'top')])}.o"],
        )
        group = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/lib.pkg.mod=lib/pkg/mod.py")
        self.assertEqual(keys(group), [
            "/py_modules/lib.pkg.mod", "/py_code/lib.pkg.mod",
            "/py_modules/lib.__init__", "/py_code/lib.__init__",
        ])
        self.assertEqual(section(group, "--kvs"), [
            "resfs/src//py_modules/lib.pkg.mod=lib/pkg/mod.py",
            "resfs/src//py_modules/lib.__init__=lib/__init__.py",
        ])

        self.assertIn("$(B)/lib/x.cpp.o", lib.node_by_output(graph, "$(B)/lib/libpylib.a")["inputs"])
        global_archive = lib.node_by_output(graph, "$(B)/lib/libpylib.global.a")
        for node in objcopy_nodes(graph, "lib"):
            self.assertIn(node["outputs"][0], global_archive["inputs"])

        compile_args = args(lib.node_by_output(graph, "$(B)/lib/x.cpp.o"))
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", compile_args)
        self.assertIn("-I$(S)/contrib/libs/python/Include", compile_args)
        self.assertIn("-DUSE_PYTHON2", compile_args)
        self.assertNotIn("-DUSE_PYTHON3", compile_args)

    def test_py2_resource_of_a_copied_source_takes_the_copy_only(self):
        files = python2_base()
        files.update({
            "lib/ya.make": "PY2_LIBRARY()\nCOPY_FILE(orig.py copied.py)\nPY_SRCS(TOP_LEVEL copied.py)\nEND()\n",
            "lib/orig.py": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        res = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/copied=lib/copied.py")
        self.assertEqual(section(res, "--inputs"), ["$(B)/lib/copied.py", "$(B)/lib/copied.py.yapyc"])
        self.assertNotIn("$(S)/lib/orig.py", res["inputs"])

    def test_py23_test_builds_a_python2_and_a_python3_test_program(self):
        files = python2_base()
        for peer in ("library/python/pytest", "library/python/runtime_py3/main", "contrib/tools/python3/Modules/_sqlite"):
            files[f"{peer}/ya.make"] = STUB_LIBRARY
            files[f"{peer}/s.cpp"] = "int s;\n"
        for tool in ("tools/py3cc", "tools/py3cc/slow"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        files.update({
            "both/ya.make": "PY23_LIBRARY()\nNO_PYTHON_INCLUDES()\nPY_SRCS(m.py)\nEND()\nRECURSE_FOR_TESTS(test)\n",
            "both/m.py": "",
            "both/test/ya.make": 'PY23_TEST()\nNO_PYTHON_INCLUDES()\nTEST_SRCS(test_m.py)\nPEERDIR(both)\n'
                'IF (MODULE_TAG == "PY3TEST_PROGRAM")\nTEST_SRCS(test_py3_tag.py)\nENDIF()\nEND()\n',
            "both/test/test_py3_tag.py": "",
            "both/test/test_m.py": "",
        })
        graph = lib.make(files, "both", "-t", opensource=False)
        py2 = lib.node_by_output(graph, "$(B)/both/test/py2_both-test")
        py3 = lib.node_by_output(graph, "$(B)/both/test/both-test")
        self.assertNotIn("ld_test_node", py2["kv"])
        self.assertNotIn("ld_test_node", py3["kv"])
        self.assertIn("$(B)/both/libpyboth.global.a", py2["inputs"])
        self.assertIn("$(B)/both/test/__vcs_version__.c.py2.o", py2["outputs"] + [a for c in py2["cmds"] for a in c["cmd_args"]])
        self.assertIn("$(B)/both/test/__vcs_version__.c.py3.o", [a for c in py3["cmds"] for a in c["cmd_args"]])
        self.assertNotIn("build/rules/py2_deprecation", " ".join(py2["inputs"]))
        lib.node_by_output(graph, "$(B)/both/test/test_m.py.yapyc")
        lib.node_by_output(graph, "$(B)/both/test/test_m.py.yapyc3")
        lib.node_by_output(graph, "$(B)/both/test/test_py3_tag.py.yapyc3")
        for suite, binary in (("pytest", py2), ("py3test", py3)):
            run = lib.node_by_output(graph, f"$(B)/both/test/test-results/{suite}/meta.json")
            self.assertIn(binary["uid"], run["deps"])
            command = args(run)
            self.assertEqual(command[command.index("--sub-path") + 1], "py2test" if suite == "pytest" else "py3test")
            self.assertLess(command.index("--sub-path"), command.index("--tar"))
        for suite in ("py2_flake8", "flake8"):
            lib.node_by_output(graph, f"$(B)/both/test/test-results/{suite}/meta.json")
            lib.node_by_output(graph, f"$(B)/both/test-results/{suite}/meta.json")
        files["both/ya.make"] = files["both/ya.make"].replace("RECURSE_FOR_TESTS(test)\n", "")
        graph = lib.make(files, "both", "-t", opensource=False)
        lib.node_by_output(graph, "$(B)/both/test-results/flake8/meta.json")
        self.assertFalse(any("py2_flake8" in o for n in graph["graph"] for o in n["outputs"]))

    def test_python2_submodules_take_the_platform_peers(self):
        files = python2_base()
        for peer in ("library/python/pytest", "library/python/runtime_py3/main", "contrib/tools/python3/Modules/_sqlite"):
            files[f"{peer}/ya.make"] = STUB_LIBRARY
            files[f"{peer}/s.cpp"] = "int s;\n"
        for tool in ("tools/py3cc", "tools/py3cc/slow"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        files["build/platform/lld/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(LLD_ROOT sbr:1)\nEND()\n"
        files.update({
            "t23/ya.make": "PY23_TEST()\nTEST_SRCS(test_m.py)\nEND()\n", "t23/test_m.py": "",
            "t2/ya.make": "PY2TEST()\nTEST_SRCS(test_m.py)\nEND()\n", "t2/test_m.py": "",
        })
        for target, suite in (("t2", "pytest"), ("t23", "pytest"), ("t23", "py3test")):
            with self.subTest(target=target, suite=suite):
                cmd = args(lib.node_by_output(lib.make(files, target, "-t", opensource=False), f"$(B)/{target}/test-results/{suite}/meta.json"))
                self.assertIn("LLD_ROOT_RESOURCE_GLOBAL", [cmd[i + 1].split("::")[0] for i, a in enumerate(cmd) if a == "--global-resource"])

    def test_py_srcs_scope_directives_apply_to_the_following_sources(self):
        files = python2_base()
        files.update({
            "lib/ya.make": "PY2_LIBRARY()\nPY_SRCS(a.py TOP_LEVEL b.py NAMESPACE n c.py)\nEND()\n",
            "lib/a.py": "",
            "lib/b.py": "",
            "lib/c.py": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        node = objcopy_with_kv(graph, "lib", "resfs/src//py_modules/lib.a=lib/a.py")
        self.assertEqual(section(node, "--kvs"), [
            "resfs/src//py_modules/lib.a=lib/a.py",
            "resfs/src//py_modules/b=lib/b.py",
            "resfs/src//py_modules/n.c=lib/c.py",
        ])

    def test_py2_cython_uses_the_py2_cython(self):
        files = python2_base()
        files.update({
            "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}PY_SRCS(TOP_LEVEL m.pyx)\nEND()\n",
            "lib/m.pyx": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(main.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/main.cpp": "int main(){return 0;}\n",
        })
        files["contrib/tools/cython_py2/cython.py"] = ""
        for name in CYTHON_UTILITY:
            files[f"contrib/tools/cython_py2/Cython/Utility/{name}"] = ""
        for path in CYTHON_HEADERS:
            files[path] = ""
        graph = lib.make(files, "prog")
        cy = lib.node_by_output(graph, "$(B)/lib/m.pyx.py2.cpp")
        self.assertEqual(cy["kv"]["p"], "CY")
        cy_args = args(cy)
        self.assertEqual(cy_args[1], "$(S)/contrib/tools/cython_py2/cython.py")
        self.assertIn("-I$(S)/contrib/tools/cython_py2/Cython/Includes", cy_args)
        self.assertNotIn("-I$(S)/contrib/tools/cython/Cython/Includes", cy_args)
        self.assertEqual(cy_args[-3:], ["$(S)/lib/m.pyx", "-o", "$(B)/lib/m.pyx.py2.cpp"])
        self.assertIn("$(S)/contrib/tools/cython_py2/Cython/Utility/Buffer.c", cy["inputs"])
        self.assertFalse([path for path in cy["inputs"] if "/contrib/tools/cython/" in path])
        compile_node = lib.node_by_output(graph, "$(B)/lib/m.pyx.py2.cpp.o")
        self.assertIn(cy["uid"], compile_node["deps"])
        lib.node_by_output(graph, "$(B)/lib/m.reg.cpp")

    def test_py2module_links_a_pic_shared_object(self):
        files = python2_base()
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
        files.update({
            "mod/ya.make": f"PY2MODULE(ext)\n{NO_PLATFORM}SRCS(ext.c)\nPEERDIR(dep)\nLDFLAGS(-Wl,--own)\nEND()\n",
            "mod/ext.c": "int ext;\n",
            "dep/ya.make": f"LIBRARY()\n{NO_PLATFORM}SRCS(d.c)\nEND()\n",
            "dep/d.c": "int d;\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(dep)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        x86_64 = ("--target-platform", "default-linux-x86_64")
        graph = lib.make(files, "mod", *x86_64)
        link = lib.node_by_output(graph, "$(B)/mod/ext.so")
        self.assertEqual(link["kv"]["p"], "LD")
        link_args = next(cmd["cmd_args"] for cmd in link["cmds"] if any(arg.endswith("link_dyn_lib.py") for arg in cmd["cmd_args"]))
        self.assertIn("$(B)/tools/fix_elf/fix_elf", link_args)
        self.assertIn("-shared", link_args)
        self.assertIn("-Wl,-soname,ext.so", link_args)
        self.assertIn("dep/libdep.a", link_args)
        tail = link_args[link_args.index("-Wl,-z,notext"):]
        self.assertEqual(tail[:2], ["-Wl,-z,notext", "-Wl,-Bsymbolic"])
        self.assertIn("-Wl,--own", tail)
        self.assertNotIn("-Wl,--version-script", " ".join(link_args))
        ext = args(lib.node_by_output(graph, "$(B)/mod/ext.c.pic.o"))
        self.assertIn("-fPIC", ext)
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", ext)
        self.assertIn("--target=x86_64-linux-gnu", ext)
        self.assertIn("-fPIC", args(lib.node_by_output(graph, "$(B)/dep/d.c.pic.o")))

        both = lib.make(files, "mod", "prog", *x86_64)
        self.assertIn("-fPIC", args(lib.node_by_output(both, "$(B)/dep/d.c.pic.o")))
        self.assertNotIn("-fPIC", args(lib.node_by_output(both, "$(B)/dep/d.c.o")))

    def test_py2module_versions_are_not_modelled(self):
        files = python2_base()
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
        files.update({
            "mod/ya.make": f"PY2MODULE(ext 1 2)\n{NO_PLATFORM}SRCS(ext.c)\nEND()\n",
            "mod/ext.c": "int ext;\n",
        })
        result = lib.make_process(files, "mod")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("versions (1) are not modelled", result.stderr)

    def test_py2_bytecode_tool_follows_use_light_py2cc(self):
        files = python2_base()
        files.update({
            "lib/ya.make": "PY2_LIBRARY()\nPY_SRCS(a.py)\nEND()\n",
            "lib/a.py": "",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        node = lib.node_by_output(graph, "$(B)/lib/a.py.yapyc")
        self.assertEqual(args(node)[0], "$(B)/tools/py2cc/py2cc")

    def test_py2_register_uses_the_py2_generator_and_init_defines(self):
        files = python2_base()
        files.update({
            "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}PY_REGISTER(my.module other)\nEND()\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        reg = lib.node_by_output(graph, "$(B)/lib/my.module.reg.cpp")
        self.assertEqual(
            args(reg)[1:],
            ["$(S)/build/scripts/gen_py_reg.py", "my.module", "$(B)/lib/my.module.reg.cpp"],
        )
        self.assertEqual(reg["inputs"], ["$(S)/build/scripts/gen_py_reg.py"])
        first = args(lib.node_by_output(graph, "$(B)/lib/my.module.reg.cpp.o"))
        self.assertNotIn("-Dinitmodule=init2my6module", first)
        second = args(lib.node_by_output(graph, "$(B)/lib/other.reg.cpp.o"))
        self.assertIn("-Dinitmodule=init2my6module", second)
        self.assertIn("-Dinit_module_module=init_module_2my6module", second)

    def test_py2_consumers_select_the_py2_variants_of_multimodules(self):
        files = python2_base()
        files.update({
            "p23/ya.make": "PY23_LIBRARY()\nNO_PYTHON_INCLUDES()\nSRCS(x.cpp)\nPY_REGISTER(p23.mod)\nEND()\n",
            "p23/x.cpp": "int x;\n",
            "n23/ya.make": "PY23_NATIVE_LIBRARY()\nSRCS(GLOBAL y.cpp)\nEND()\n",
            "n23/y.cpp": "int y;\n",
            "py2/ya.make": "PY2_LIBRARY()\nNO_PYTHON_INCLUDES()\nPEERDIR(p23 n23)\nEND()\n",
            "plain/ya.make": f"LIBRARY()\n{NO_PLATFORM}PEERDIR(p23 n23)\nEND()\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(py2 plain)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")

        py2 = lib.node_by_output(graph, "$(B)/p23/libpyp23.a")
        self.assertIn("$(B)/p23/x.cpp.py2.o", py2["inputs"])
        py2_args = args(lib.node_by_output(graph, "$(B)/p23/x.cpp.py2.o"))
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", py2_args)
        self.assertIn("-Dinitmod=init3p233mod", py2_args)
        self.assertNotIn("-DUSE_PYTHON3", py2_args)
        lib.node_by_output(graph, "$(B)/p23/p23.mod.reg.cpp")
        py3_args = args(lib.node_by_output(graph, "$(B)/p23/x.cpp.py3.o"))
        self.assertIn("-DUSE_PYTHON3", py3_args)
        self.assertIn("-DPyInit_mod=PyInit_3p233mod", py3_args)
        lib.node_by_output(graph, "$(B)/p23/libpy3p23.a")

        native = lib.node_by_output(graph, "$(B)/n23/libn23.global.a")
        self.assertIn("$(B)/n23/y.cpp.py2.o", native["inputs"])
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", args(lib.node_by_output(graph, "$(B)/n23/y.cpp.py2.o")))
        lib.node_by_output(graph, "$(B)/n23/libpy3cn23.global.a")

    def test_py2_variant_sees_the_py2_module_tag(self):
        files = python2_base()
        files.update({
            "lib/ya.make": f"PY2_LIBRARY()\n{NO_PLATFORM}SRCS(x.cpp)\nEND()\n",
            "lib/x.cpp": "int x;\n",
            "prog/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n",
            "prog/m.cpp": "int main(){return 0;}\n",
        })
        graph = lib.make(files, "prog")
        link = lib.node_by_output(graph, "$(B)/prog/prog")
        self.assertIn("$(B)/lib/libpylib.a", link["inputs"])
        self.assertIn("-DUSE_PYTHON2", args(lib.node_by_output(graph, "$(B)/lib/x.cpp.o")))
        self.assertFalse([node for node in graph["graph"] if "runtime_py3" in node["outputs"][0]])

    def test_py2_program_links_the_python2_runtime(self):
        files = python2_base()
        files.update({
            "prog/ya.make": (
                "PY2_PROGRAM(prog)\n"
                "NO_PYTHON_INCLUDES()\n"
                "ENABLE(USE_LIGHT_PY2CC)\n"
                "PY_SRCS(__main__.py)\n"
                "END()\n"
            ),
            "prog/__main__.py": "",
        })
        for library_peers, opensource in (
            (["build/rules/py2_deprecation"], False),
            ([], True),
        ):
            with self.subTest(opensource=opensource):
                graph = lib.make(files, "prog", "-r", opensource=opensource)
                link = lib.node_by_output(graph, "$(B)/prog/prog")
                peers = [
                    path.rsplit("/", 1)[0][len("$(B)/"):]
                    for path in link["inputs"]
                    if path.endswith(".a")
                ]
                expected = [
                    "library/cpp/malloc/jemalloc",
                    "library/python/runtime/main",
                    "contrib/deprecated/python/subprocess32",
                    "contrib/tools/python/src/Modules/_sqlite",
                    "library/python/import_tracing/constructor",
                ] + library_peers + [
                    "library/cpp/resource",
                    "library/python/testing/import_test",
                ]
                self.assertEqual([peer for peer in peers if peer in expected], expected)
                link_args = next(
                    cmd["cmd_args"] for cmd in link["cmds"]
                    if any(arg.endswith("link_exe.py") for arg in cmd["cmd_args"])
                )
                self.assertIn("-Wl,--strip-all", link_args)
                main = objcopy_with_kv(graph, "prog", "resfs/src//py_modules/__main__=prog/__main__.py")
                self.assertEqual(keys(main), ["/py_modules/__main__", "/py_code/__main__"])
                self.assertEqual(args(lib.node_by_output(graph, "$(B)/prog/__main__.py.yapyc"))[0], STAGE0)

    def test_py2test_is_a_python2_program_with_pytest_first(self):
        # PY2TEST is PYTEST_BIN over _BASE_PYTEST over _BASE_PY_PROGRAM
        # (build/conf/python.conf): a Python 2 program whose pytest peer
        # precedes the _BASE_PY_PROGRAM runtime peers.
        files = python2_base()
        files.update({
            "library/python/pytest/ya.make": STUB_LIBRARY,
            "library/python/pytest/s.cpp": "int s;\n",
            "p23/ya.make": "PY23_LIBRARY()\nNO_PYTHON_INCLUDES()\nPY_SRCS(m.py)\nEND()\n",
            "p23/m.py": "",
            "t/ya.make": "PY2TEST()\nNO_PYTHON_INCLUDES()\nTEST_SRCS(test_a.py)\nPEERDIR(p23)\nEND()\n",
            "t/test_a.py": "",
        })
        graph = lib.make(files, "t", "-r", opensource=False)
        link = lib.node_by_output(graph, "$(B)/t/t")
        peers = [path.rsplit("/", 1)[0][len("$(B)/"):] for path in link["inputs"] if path.endswith(".a")]
        expected = [
            "library/python/pytest",
            "library/python/runtime/main",
            "contrib/deprecated/python/subprocess32",
            "contrib/tools/python/src/Modules/_sqlite",
            "library/python/import_tracing/constructor",
            "build/rules/py2_deprecation",
            "library/python/testing/import_test",
        ]
        self.assertEqual([peer for peer in peers if peer in expected], expected)
        self.assertIn("$(B)/p23/libpyp23.global.a", link["inputs"])
        lib.node_by_output(graph, "$(B)/p23/m.py.yapyc")
        lib.node_by_output(graph, "$(B)/t/test_a.py.yapyc")
        objcopy_with_kv(graph, "t", "resfs/src//py_modules/__tests__.test_a=t/test_a.py")


if __name__ == "__main__":
    unittest.main(verbosity=2)
