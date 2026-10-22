import unittest

import lib
from test_codegen_cuda import cuda_conf


PROXY = "contrib/libs/openmp"
ENGINE = "parallel/kite"
OPENMP_FLAGS = ["-fopenmp=libomp", "-fopenmp-version=52"]


def library(body):
    return f"LIBRARY()\n{lib.BARE_MODULE}{body}END()\n"


def fixture(setting="SET(USE_OPENMP yes)\n", sources=True):
    # ymake.core.conf:2733-2735 selects this proxy. Keep its compiler branches
    # from contrib/libs/openmp/ya.make, with a renamed, tiny runtime peer.
    proxy = (
        "IF(MSVC OR CLANG_CL)\n"
        'MESSAGE(WARNING "OpenMP runs sequentially on this compiler")\n'
        "ELSEIF(CLANG)\n"
        "CFLAGS(GLOBAL -fopenmp=libomp GLOBAL -fopenmp-version=52)\n"
        f"PEERDIR({ENGINE})\n"
        "ELSEIF(GCC)\nCFLAGS(GLOBAL -fopenmp)\n"
        f"PEERDIR({ENGINE})\n"
        'ELSE()\nMESSAGE(FATAL_ERROR "Current compiler does not support OpenMP")\nENDIF()\n'
    )
    files = {
        f"{PROXY}/ya.make": library(proxy),
        f"{ENGINE}/ya.make": library(
            f"ADDINCL(GLOBAL {ENGINE}/include GLOBAL ${{ARCADIA_BUILD_ROOT}}/{ENGINE})\n"
            "CFLAGS(-DKITE_PRIVATE)\nCOPY_FILE(seed.h runtime_config.h)\nSRCS(engine.cpp)\n"
        ),
        f"{ENGINE}/engine.cpp": "",
        f"{ENGINE}/include/parallel_api.h": '#include "runtime_config.h"\n',
        f"{ENGINE}/seed.h": "#define KITE_WIDTH 4\n",
        "explicit/fern/ya.make": library(
            "ADDINCL(GLOBAL explicit/fern/include)\nCFLAGS(GLOBAL -DFERN_PEER)\n"
        ),
        "explicit/fern/include/fern.h": "",
        "worker/ya.make": library(
            "PEERDIR(explicit/fern)\nCFLAGS(-DWORKER_LOCAL)\n"
            + ("SRCS(plain.cpp plain.c)\n" if sources else "") + setting
        ),
        "worker/plain.cpp": "",
        "worker/plain.c": "",
        "build/scripts/fs_tools.py": "",
    }
    return files


def args(graph, output="$(B)/worker/plain.cpp.o"):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


class OpenMPConfiguredPeerTest(unittest.TestCase):
    def test_final_module_value_selects_proxy_before_explicit_peers(self):
        files = fixture("SET(USE_OPENMP no)\nSET(USE_OPENMP yes)\n")
        files["worker/plain.cpp"] = '#include "parallel_api.h"\n'
        files["launcher/ya.make"] = (
            f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(worker)\nSRCS(start.cpp)\nEND()\n"
        )
        files["launcher/start.cpp"] = "int main() {}\n"
        graph = lib.make(files, "launcher")
        cc = lib.node_by_output(graph, "$(B)/worker/plain.cpp.o")
        command = args(graph)
        for flag in OPENMP_FLAGS:
            self.assertEqual(command.count(flag), 1)
        self.assertLess(command.index(f"-I$(S)/{ENGINE}/include"),
                        command.index("-I$(S)/explicit/fern/include"))
        self.assertLess(command.index(OPENMP_FLAGS[0]), command.index("-DFERN_PEER"))
        self.assertIn("-DWORKER_LOCAL", command)
        self.assertNotIn("-DKITE_PRIVATE", command)
        copy = lib.node_by_output(graph, f"$(B)/{ENGINE}/runtime_config.h")
        self.assertIn(f"$(S)/{ENGINE}/seed.h", copy["inputs"])
        self.assertIn(f"$(S)/{ENGINE}/include/parallel_api.h", cc["inputs"])
        self.assertIn(f"$(B)/{ENGINE}/runtime_config.h", cc["inputs"])
        self.assertIn(copy["uid"], cc["deps"])
        link = lib.node_by_output(graph, "$(B)/launcher/launcher")
        runtime = lib.node_by_output(graph, f"$(B)/{ENGINE}/libparallel-kite.a")
        self.assertIn(runtime["outputs"][0], link["inputs"])
        self.assertIn(runtime["uid"], link["deps"])
        self.assertIn("-DKITE_PRIVATE", args(graph, f"$(B)/{ENGINE}/engine.cpp.o"))
        for output in ("$(B)/worker/plain.c.o", "$(B)/launcher/start.cpp.o"):
            self.assertTrue(all(flag in args(graph, output) for flag in OPENMP_FLAGS))

    def test_condition_requires_exact_yes_and_final_override(self):
        for value in ("", "no", "YES", "true", "1", "yes later"):
            with self.subTest(value=value):
                files = fixture(f"SET(USE_OPENMP yes)\nSET(USE_OPENMP {value})\n")
                del files[f"{PROXY}/ya.make"]
                graph = lib.make(files, "worker")
                self.assertFalse(any(flag in args(graph) for flag in OPENMP_FLAGS))
                self.assertIn("-DFERN_PEER", args(graph))
                self.assertFalse(any(f"$(B)/{ENGINE}/libparallel-kite.a" in n["outputs"]
                                     for n in graph["graph"]))

    def test_emitted_configuration_expands_nonliteral_switch(self):
        for value, enabled in (("yes", True), ("no", False)):
            with self.subTest(value=value):
                files = fixture("SET(USE_OPENMP ${PARALLEL_CHOICE})\n")
                files["build/ymake_conf.py"] = lib.ymake_conf(
                    extra="PARALLEL_CHOICE=$PARALLEL_DEFAULT\nPARALLEL_DEFAULT=yes\n"
                )
                graph = lib.make(files, "worker", f"-DPARALLEL_DEFAULT={value}")
                self.assertEqual(all(flag in args(graph) for flag in OPENMP_FLAGS), enabled)

    def test_configured_switch_and_local_override(self):
        for emitted, option in (("", "USE_OPENMP"),
                                ("USE_OPENMP=$PARALLEL_SWITCH\n", "PARALLEL_SWITCH")):
            for value in ("yes", "no"):
                with self.subTest(option=option, value=value):
                    files = fixture("")
                    files["build/ymake_conf.py"] = lib.ymake_conf(extra=emitted)
                    # These peers explicitly opt out of the global switch. The
                    # consumer inherits it; no module path exempts a module.
                    for directory in (PROXY, ENGINE, "explicit/fern"):
                        files[f"{directory}/ya.make"] = files[f"{directory}/ya.make"].replace(
                            "LIBRARY()\n", "LIBRARY()\nSET(USE_OPENMP no)\n", 1
                        )
                    graph = lib.make(files, "worker", f"-D{option}={value}")
                    self.assertEqual(all(flag in args(graph) for flag in OPENMP_FLAGS), value == "yes")
                    files["worker/ya.make"] = library("SET(USE_OPENMP no)\nSRCS(plain.cpp)\n")
                    graph = lib.make(files, "worker", f"-D{option}={value}")
                    self.assertFalse(any(flag in args(graph) for flag in OPENMP_FLAGS))

    def test_compiler_behavior_is_exported_by_the_proxy_source(self):
        for flags, expected, runtime in (
            ([], OPENMP_FLAGS, True),
            (["-DCLANG=no", "-DGCC=yes"], ["-fopenmp"], True),
            (["-DMSVC=yes"], [], False),
            (["-DCLANG_CL=yes"], [], False),
        ):
            with self.subTest(flags=flags):
                files = fixture()
                files[f"{PROXY}/ya.make"] = files[f"{PROXY}/ya.make"].replace(
                    "END()", "CFLAGS(GLOBAL -DPROXY_SOURCE_EXPORT)\nEND()"
                )
                graph = lib.make(files, "worker", *flags)
                self.assertEqual([f for f in args(graph) if f.startswith("-fopenmp")], expected)
                self.assertIn("-DPROXY_SOURCE_EXPORT", args(graph))
                self.assertEqual(f"-I$(S)/{ENGINE}/include" in args(graph), runtime)

    def test_target_and_host_use_their_own_configured_values(self):
        for target, host in (("yes", "no"), ("no", "yes")):
            with self.subTest(target=target, host=host):
                files = fixture("SET(USE_OPENMP ${PARALLEL_CHOICE})\n")
                files["ya.conf"] = (
                    f'[flags]\nOPENSOURCE="yes"\nPARALLEL_CHOICE="{target}"\n'
                    f'[host_platform_flags]\nOPENSOURCE="yes"\nPARALLEL_CHOICE="{host}"\n'
                )
                lib.tool_program(files, "tools/parallel_helper", "parallel_helper")
                files["tools/parallel_helper/ya.make"] = files["tools/parallel_helper/ya.make"].replace(
                    "SRCS(main.cpp)", "SET(USE_OPENMP ${PARALLEL_CHOICE})\nSRCS(main.cpp)"
                )
                files["worker/ya.make"] = files["worker/ya.make"].replace(
                    "END()", "RUN_PROGRAM(tools/parallel_helper OUT generated.cpp)\nEND()"
                )
                graph = lib.make(files, "worker")
                self.assertEqual(all(flag in args(graph) for flag in OPENMP_FLAGS), target == "yes")
                helper = args(graph, "$(B)/tools/parallel_helper/main.cpp.pic.o")
                self.assertEqual(all(flag in helper for flag in OPENMP_FLAGS), host == "yes")
                generated = lib.node_by_output(graph, "$(B)/worker/generated.cpp")
                tool = lib.node_by_output(graph, "$(B)/tools/parallel_helper/parallel_helper")
                self.assertIn(tool["uid"], generated["deps"])

    def test_cpp_and_cuda_keep_peer_flags_and_generated_header_dependencies(self):
        files = fixture()
        files["build/ymake_conf.py"] = cuda_conf()
        files["worker/ya.make"] = files["worker/ya.make"].replace(
            "END()", "SET(CUDA_ARCHITECTURES sm_80:sm_90a)\nCUDA_SRCS(sub/kernel.cu)\nEND()"
        )
        files["worker/sub/kernel.cu"] = '#include "parallel_api.h"\n'
        files["build/internal/platform/cuda/cuda_runtime_include.h"] = ""
        files["build/scripts/compile_cuda.py"] = ""
        files["build/scripts/fatbinary_wrapper.py"] = ""
        lib.tool_program(files, "tools/mtime0", "mtime0")
        lib.tool_program(files, "tools/custom_pid", "custom_pid")
        graph = lib.make(files, "worker", "--target-platform", "default-linux-x86_64")
        copy = lib.node_by_output(graph, f"$(B)/{ENGINE}/runtime_config.h")
        for suffix in ("80.cubin", "90a.cubin", "cudafe1.cpp"):
            node = lib.node_by_output(graph, f"$(B)/worker/sub/kernel.{suffix}")
            command = node["cmds"][0]["cmd_args"]
            for flag in OPENMP_FLAGS:
                self.assertIn(flag, command)
            self.assertLess(command.index(f"-I$(S)/{ENGINE}/include"),
                            command.index("-I$(S)/explicit/fern/include"))
            self.assertIn(f"$(S)/{ENGINE}/include/parallel_api.h", node["inputs"])
            self.assertIn(f"$(B)/{ENGINE}/runtime_config.h", node["inputs"])
            self.assertIn(copy["uid"], node["deps"])
        self.assertTrue(all(flag in args(graph) for flag in OPENMP_FLAGS))

    def test_shared_lexical_include_uses_each_modules_final_environment(self):
        files = fixture()
        files["shared/parallel.inc"] = "SET(USE_OPENMP ${PARALLEL_CHOICE})\n"
        for directory, value in (("enabled", "yes"), ("disabled", "no")):
            files[f"{directory}/ya.make"] = library(
                f"SET(PARALLEL_CHOICE {value})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/parallel.inc)\nSRCS(unit.cpp)\n"
            )
            files[f"{directory}/unit.cpp"] = ""
        graph = lib.make(files, "disabled", "enabled")
        for directory, enabled in (("enabled", True), ("disabled", False)):
            command = args(graph, f"$(B)/{directory}/unit.cpp.o")
            self.assertEqual(all(flag in command for flag in OPENMP_FLAGS), enabled)


if __name__ == "__main__":
    unittest.main(verbosity=2)
