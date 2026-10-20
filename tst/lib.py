import json
import os
import subprocess
import tempfile
from pathlib import Path


AY = Path(os.environ["AY_TEST_BINARY"])


# A reduced build/ymake_conf.py: the real script's rules for the variables ay
# reads, the same command line and the same output forms. COMPRESS_DEBUG and
# EXTRA are the knobs tests turn; ymake_conf() renders a variant.
FAKE_YMAKE_CONF = r'''
import base64
import json
import os
import sys

COMPRESS_DEBUG = False
EXTRA = ""


def emit(key, *value):
    print("{}={}".format(key, " ".join(str(v) for v in value if v is not None)))


def platform(data):
    os_ = data["os"].lower()
    os_ = {"darwin": "macos", "win": "windows"}.get(os_, os_)
    arch = data["arch"].lower()
    p = {"os": os_, "arch": arch}
    p["x86_64"] = arch in ("x86_64", "amd64")
    p["armv8"] = arch in ("armv8", "armv8a", "arm64", "aarch64")
    p["linux"] = os_ == "linux"
    p["macos"] = os_ == "macos"
    p["posix"] = os_ in ("linux", "macos", "ios", "android", "freebsd")
    p["os_compat"] = "DARWIN" if p["macos"] else os_.upper()
    os_vars = [os_.upper(), "OS_" + os_.upper()] + (["DARWIN", "OS_DARWIN"] if p["macos"] else [])
    arch_vars = [v for on, v in (
        (p["x86_64"], "ARCH_X86_64"),
        (p["armv8"], "ARCH_ARM64"),
        (p["armv8"], "ARCH_ARM"),
        (p["armv8"] and (p["linux"] or p["macos"]), "ARCH_AARCH64"),
        (True, "ARCH_TYPE_64"),
    ) if on]
    p["vars"] = os_vars, arch_vars
    return p


def main(argv):
    presets, args, params, local, i = {}, [], None, False, 1
    while i < len(argv):
        if argv[i] == "--toolchain-params":
            params = json.loads(base64.b64decode(argv[i + 1]))
            i += 2
        elif argv[i] == "-D":
            key, _, value = argv[i + 1].partition("=")
            presets[key] = value
            i += 2
        elif argv[i] == "-l":
            local = True
            i += 1
        else:
            args.append(argv[i])
            i += 1
    for key in ("CFLAGS", "CXXFLAGS", "CONLYFLAGS", "LDFLAGS", "GO_COMPILE_FLAGS", "GO_LINK_FLAGS", "STD_CXX_VERSION"):
        presets["USER_" + key] = " ".join(filter(None, [presets.pop(key, None), presets.get("USER_" + key)]))
    build_type = args[1].lower()
    if build_type.startswith("dist-"):
        build_type = build_type[len("dist-"):]
    release = build_type in ("release", "relwithdebinfo", "minsizerel", "profile", "gprof") or build_type.endswith("-release")
    sanitizer = presets.get("SANITIZER_TYPE", "")
    sanitized = bool(sanitizer) and sanitizer.lower() not in ("no", "false", "off", "0")
    host = platform(params["platform"]["host"])
    target = platform(params["platform"]["target"])
    tc = params["params"]

    print('@import "${CONF_ROOT}/ymake.core.conf"')
    if os.path.exists(os.path.join(args[0], "build/internal/conf/internal.conf")):
        print('@import "${CONF_ROOT}/internal/conf/internal.conf"')
    emit("BUILD_TYPE", build_type.upper())
    emit("BT_" + build_type.upper().replace("-", "_"), "yes")
    emit("DISTBUILD", "yes")
    emit("HOST_PLATFORM", host["os_compat"])
    for var in host["vars"][0] + host["vars"][1]:
        emit("HOST_" + var, "yes")
    emit("TARGET_PLATFORM", target["os_compat"])
    emit("HARDWARE_TYPE", target["arch"])
    for var in target["vars"][1] + target["vars"][0]:
        emit(var, "yes")
    if target["posix"]:
        emit("UNIX", "yes")
    if presets.get("FORCE_NO_PIC", "").lower() not in ("yes", "true", "on", "1"):
        emit("PIC", "yes")
    debug = build_type in ("debug", "debugnoasserts", "fastdebug") or build_type.endswith("-debug")
    if debug:
        emit("_BUILD_DEBUG", "yes")
    if release:
        emit("_BUILD_RELEASE", "yes")
    if sanitized:
        emit("_BUILD_SANITIZED", "yes")
    os_sdk = presets.get("OS_SDK") or ("ubuntu-16" if target["linux"] else None)
    if "OS_SDK" not in presets:
        emit("OS_SDK", os_sdk)
    emit("CLANG", "yes")
    emit("COMPILER_ID", tc["type"].upper())
    emit("COMPILER_VERSION", tc["version"].split(".")[0])
    emit("_DEFAULT_LINKER_ID", "lld")
    debug_info = ["-g"]
    if COMPRESS_DEBUG and not release and target["linux"]:
        debug_info.append("-gz=zstd")
    debug_info.append("-fdebug-default-version=4")
    if target["linux"]:
        debug_info.append("-ggnu-pubnames")
        if release:
            debug_info.append("-fdebug-info-for-profiling")
    emit("_DEBUG_INFO_FLAGS", *debug_info)
    c_flags = ["$CL_DEBUG_INFO", "$CL_DEBUG_INFO_DISABLE_CACHE__NO_UID__", "-pipe"]
    if target["x86_64"]:
        c_flags.append("-m64")
    c_foptions = ["-fno-common", "-ffunction-sections", "-fdata-sections"]
    if target["armv8"]:
        c_foptions.append("-fsigned-char")
    c_foptions += ["-fsized-deallocation", "-fexceptions"]
    if target["linux"]:
        c_foptions.append("-fuse-init-array")
    c_foptions += ["-fcolor-diagnostics", "-faligned-allocation"]
    if debug:
        c_foptions.append("$FSTACK")
    if release:
        c_flags.append("$OPTIMIZE")
    emit("OPTIMIZE", "-O3" if release else None)
    emit("_C_FLAGS", *c_flags)
    emit("_C_FOPTIONS", *c_foptions)
    print("C_DEFINES+=-D_THREAD_SAFE -D_PTHREADS")
    print('@import "${CONF_ROOT}/conf/compilers/gnu_compiler.conf"')
    emit("OBJCOPY_TOOL_VENDOR", tc.get("objcopy"))
    ld_flags = []
    if COMPRESS_DEBUG and not release and target["linux"]:
        ld_flags.append("-Wl,--compress-debug-sections=zstd")
    if presets.get("MUSL") == "yes":
        ld_flags.append("-Wl,--no-as-needed")
    elif target["linux"]:
        ld_flags.extend(["-ldl", "-lrt", "-Wl,--no-as-needed"])
    emit("_LD_FLAGS", *ld_flags)
    optimized = release and not sanitized
    emit("USE_LOCAL_TOOLS", "yes" if local else "no")
    emit("RLGEN_FLAGS", "-G2" if optimized else "-T0")
    emit("RAGEL6_FLAGS", "-CG2" if optimized else "-CT0")
    if "HAVE_CUDA" not in presets:
        have_cuda = (presets.get("MUSL") != "yes" and not sanitized and host["linux"] and host["x86_64"]
                     and target["linux"] and target["x86_64"])
        emit("HAVE_CUDA", "yes" if have_cuda else "no")
        if have_cuda:
            emit("CUDA_ROOT", "$CUDA_RESOURCE_GLOBAL")
            emit("CUDA_VERSION", "12.9")
    print("# IGNORE YMAKE CONF CONTEXT")
    emit("SVN_DEPENDS_CACHE__NO_UID__", '${hide;kv:"disable_cache"}' if local else None)
    sys.stdout.write(EXTRA)
    print("# Variables set from command line by -D options")
    for key in sorted(presets):
        emit(key, presets[key])


main(sys.argv)
'''


def ymake_conf(compress_debug=False, extra=""):
    """The fake build/ymake_conf.py, optionally compressing debug info or printing extra lines."""
    return (
        FAKE_YMAKE_CONF
        .replace("COMPRESS_DEBUG = False", f"COMPRESS_DEBUG = {compress_debug}")
        .replace('EXTRA = ""', f"EXTRA = {extra!r}")
    )


def ya_conf_json(version="20", formula=None):
    """A build/ya.conf.json whose clang toolchain is the default on every platform the tests name."""
    platforms = [
        ("LINUX", "x86_64"), ("LINUX", "aarch64"), ("DARWIN", "x86_64"), ("DARWIN", "arm64"),
        ("WINDOWS", "x86_64"), ("IOS", "arm64"),
    ]
    return json.dumps({
        "toolchain": {
            f"clang{version}": {
                "tools": {"c++": {"bottle": f"clang{version}", "executable": "c++"}},
                "platforms": [
                    {"host": {"os": hos, "arch": harch}, "target": {"os": tos, "arch": tarch}, "default": True}
                    for hos, harch in platforms for tos, tarch in platforms
                ],
                "params": {
                    "type": "clang",
                    "match_root": "CLANG",
                    "use_bundle": True,
                    "c_compiler": "$(CLANG)/bin/clang",
                    "cxx_compiler": "$(CLANG)/bin/clang++",
                    "objcopy": "$(CLANG)/bin/llvm-objcopy",
                    "version": version,
                },
                "env": {"CPATH": [""], "LIBRARY_PATH": [""], "SDKROOT": [""]},
            },
        },
        "bottles": {
            f"clang{version}": {
                "formula": formula or {"by_platform": {"linux-x86_64": {"uri": "sbr:1"}, "darwin-arm64": {"uri": "sbr:2"}}},
            },
        },
    }, indent=1) + "\n"


# The command parts consumed from ld.conf, with its Linux/Darwin conditions.
FAKE_LD_CONF = r'''
_START_GROUP=
_END_GROUP=
_SONAME_OPTION=
LD_STRIP_FLAG=
_LD_SRCS_GLOBALS=--start-wa ${rootrel;ext=.a:SRCS_GLOBAL} --end-wa ${rootrel;ext=.o:SRCS_GLOBAL} ${rootrel;ext=.supp:SRCS_GLOBAL}
when ($OS_LINUX == "yes") {
    _START_GROUP=-Wl,--start-group
    _END_GROUP=-Wl,--end-group
    _SONAME_OPTION=-soname
    LD_STRIP_FLAG=-Wl,--strip-all
    _LD_SRCS_GLOBALS=-Wl,--whole-archive --ya-start-command-file ${rootrel;ext=.a:SRCS_GLOBAL} --ya-end-command-file -Wl,--no-whole-archive ${rootrel;ext=.o:SRCS_GLOBAL} ${rootrel;ext=.supp:SRCS_GLOBAL}
}
when ($OS_DARWIN == "yes") {
    _SONAME_OPTION=-install_name
}
_LD_DCE_FLAG_GC_SECTIONS=
when ($OS_DARWIN == "yes") {
    _LD_DCE_FLAG_GC_SECTIONS=-Wl,-dead_strip
}
elsewhen ($OS_LINUX == "yes") {
    _LD_DCE_FLAG_GC_SECTIONS=-Wl,--gc-sections
}
LD_DCE_FLAG =
LD_DCE_FLAG+=$_LD_DCE_FLAG_GC_SECTIONS
_LD_ICF_FLAG=
when ($_DEFAULT_LINKER_ID == "lld") {
    _LD_ICF_FLAG=-Wl,--icf=safe
}
LD_ICF_FLAG=$_LD_ICF_FLAG
_LD_NO_PIE_FLAG=
when ($_LINKER_ID == "lld" && $PIE != "yes" && ($PIC != "yes" || $PIC_NO_PIE == "yes")) {
    when ($OS_LINUX == "yes") {
        _LD_NO_PIE_FLAG=-Wl,-no-pie
    }
}
_LD_USE_STDLIB=
when ($_BUILD_COVERAGE == "yes" || $GCOV_COVERAGE == "yes" || $_BUILD_SANITIZED == "yes") {
    _LD_USE_STDLIB=
}
elsewhen ($OS_LINUX == "yes" || $OS_DARWIN == "yes") {
    _LD_USE_STDLIB=-nodefaultlibs
}
when ($OS_LINUX == "yes" && $MUSL != "yes") {
    _C_SYSTEM_LIBRARIES=$_LD_USE_STDLIB -lpthread -lc
}
when ($MUSL == "yes") {
    _C_SYSTEM_LIBRARIES=-nostdlib
}
C_SYSTEM_LIBRARIES=$_C_SYSTEM_LIBRARIES
LD_EXPORT_ALL_DYNAMIC_SYMBOLS_FLAG=
when ($OS_LINUX == "yes") {
    LD_EXPORT_ALL_DYNAMIC_SYMBOLS_FLAG=-rdynamic
}
EXPORTS_VALUE=$LD_EXPORT_ALL_DYNAMIC_SYMBOLS_FLAG
when ($EXPORTS_FILE) {
    EXPORTS_VALUE=$LD_EXPORT_ALL_DYNAMIC_SYMBOLS_FLAG -Wl,--version-script=${input:EXPORTS_FILE}
}
when ($NO_EXPORT_DYNAMIC_SYMBOLS == "yes") {
    EXPORTS_VALUE=
}
LINKER_SCRIPT_VALUE=${ext=.ld;pre=-T:SRCS_GLOBAL}
_SONAME_FLAG=-Wl,$_SONAME_OPTION,$_SONAME
_SHARED_FLAG=-shared
_ROOT_FLAGS=--source-root $ARCADIA_ROOT --build-root $(BUILD_ROOT)
'''


# The sysincl configs the tests write, in the shape of build/conf/sysincl.conf.
FAKE_SYSINCL_CONF = """SYSINCL=
SYSINCL+=build/sysincl/macro.yml
SYSINCL+=build/sysincl/windows.yml
SYSINCL+=build/sysincl/misc.yml
"""


CONF_FILES = {
    "build/ymake_conf.py": FAKE_YMAKE_CONF,
    "build/ya.conf.json": ya_conf_json(),
    "build/ymake.core.conf": '@import "${CONF_ROOT}/conf/sysincl.conf"\n',
    "build/conf/sysincl.conf": FAKE_SYSINCL_CONF,
    "build/sysincl/macro.yml": "- includes: []\n",
    "build/sysincl/windows.yml": "- includes: []\n",
    "build/sysincl/misc.yml": "- includes: []\n",
    "build/conf/compilers/gnu_compiler.conf": "",
    "build/conf/linkers/ld.conf": FAKE_LD_CONF,
    "build/config/tests/py_style/default_configs.json": '{"py2_flake8":"build/config/tests/flake8/flake8.conf"}',
    "build/external_resources/flake8_py2/ya.make": (
        "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(FLAKE8_PY2 sbr:123)\nEND()\n"
    ),
}


def seed_root(root):
    """Marks root as a source root with the configuration files ay make runs."""
    (root / ".arcadia.root").touch()
    for relative, content in CONF_FILES.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)


TOOLCHAIN_ENV_VARS = {
    "AR", "CC", "CFLAGS", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS",
    "CGO_LDFLAGS", "CPP", "CPPFLAGS", "CXX", "CXXFLAGS", "LD", "LDFLAGS",
    "LDLIBS", "NIX_CFLAGS_COMPILE", "NIX_CFLAGS_LINK", "NIX_LDFLAGS", "NM",
    "OBJCOPY", "RANLIB", "STRIP",
}


def run_process(*args, timeout=10, env=None):
    return subprocess.run(
        [str(AY), *map(str, args)],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


def run(*args, timeout=10, env=None):
    result = run_process(*args, timeout=timeout, env=env)
    if result.returncode != 0:
        raise AssertionError(
            f"command failed with exit code {result.returncode}: {result.args!r}\n"
            f"--- stdout ---\n{result.stdout}"
            f"--- stderr ---\n{result.stderr}"
        )
    return result


def make_process(files, target, *args, opensource=True, env=None, timeout=10):
    """Generates the graph of target in a fresh tree; returns the finished process."""
    with tempfile.TemporaryDirectory(prefix="ay-make-test-") as directory:
        root = Path(directory)
        seed_root(root)
        value = 'OPENSOURCE = "yes"\n' if opensource else ""
        (root / "ya.conf").write_text(
            f"[flags]\n{value}\n[host_platform_flags]\n{value}"
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            **{
                key: value
                for key, value in os.environ.items()
                if key not in TOOLCHAIN_ENV_VARS
            },
            **(env or {}),
        }
        return run_process(
            "make", "-j0", "-G", "--sandboxing",
            "--source-root", root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
            *args,
            target,
            env=env,
            timeout=timeout,
        )


def make(files, target, *args, opensource=True, env=None, timeout=10):
    result = make_process(files, target, *args, opensource=opensource, env=env, timeout=timeout)
    if result.returncode != 0:
        raise AssertionError(
            f"command failed with exit code {result.returncode}: {result.args!r}\n"
            f"--- stdout ---\n{result.stdout}"
            f"--- stderr ---\n{result.stderr}"
        )
    return json.loads(result.stdout)


def node_by_output(graph, output):
    for node in graph["graph"]:
        if output in node.get("outputs", []):
            return node
    raise AssertionError(f"graph has no node producing {output!r}")


def node_by_output_prefix(graph, prefix):
    for node in graph["graph"]:
        if any(output.startswith(prefix) for output in node.get("outputs", [])):
            return node
    raise AssertionError(f"graph has no node producing prefix {prefix!r}")


def only_node_by_kind(graph, kind):
    nodes = [node for node in graph["graph"] if node.get("kv", {}).get("p") == kind]
    if len(nodes) != 1:
        raise AssertionError(f"expected one {kind} node, got {len(nodes)}")
    return nodes[0]


def tool_program(files, path, name):
    files[f"{path}/ya.make"] = (
        f"PROGRAM({name})\n"
        "NO_LIBC()\n"
        "NO_RUNTIME()\n"
        "NO_UTIL()\n"
        "SRCS(main.cpp)\n"
        "END()\n"
    )
    files[f"{path}/main.cpp"] = "int main(){return 0;}\n"


BARE_MODULE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def layered_project():
    """A program over three libraries with header chains, generated headers and a tool."""
    files = {}
    for name, peers in (("a", ""), ("b", "a"), ("c", "a b")):
        headers = [f"{name}/h{i}.h" for i in range(6)]
        for i, header in enumerate(headers):
            nested = f'#include "{headers[i + 1]}"\n' if i + 1 < len(headers) else ""
            files[header] = f"#pragma once\n{nested}"
        sources = []
        for i in range(4):
            source = f"s{i}.cpp"
            sources.append(source)
            includes = "".join(f'#include "{p}/h0.h"\n' for p in peers.split())
            files[f"{name}/{source}"] = f'{includes}#include "{headers[i]}"\nint {name}{i}(){{return 0;}}\n'
        peerdir = f"PEERDIR({peers})\n" if peers else ""
        files[f"{name}/ya.make"] = (
            f"LIBRARY()\n{BARE_MODULE}{peerdir}"
            f"RUN_PROGRAM(tools/gen OUT {name}_gen.h)\n"
            f"SRCS({' '.join(sources)})\nEND()\n"
        )
        files[f"{name}/s0.cpp"] = f'#include "{name}_gen.h"\n' + files[f"{name}/s0.cpp"]
    files["app/ya.make"] = (
        f"PROGRAM()\n{BARE_MODULE}PEERDIR(a b c)\n"
        "RUN_PROGRAM(tools/gen OUT gen.h)\n"
        "SRCS(main.cpp)\nEND()\n"
    )
    files["app/main.cpp"] = '#include "gen.h"\n#include "c/h0.h"\nint main(){return 0;}\n'
    tool_program(files, "tools/gen", "gen")
    return files


def wide_project(count):
    """Libraries in a peer chain, each exporting a global include directory."""
    files = {}
    for i in range(count):
        peers = " ".join(f"l{j}" for j in range(max(0, i - 3), i))
        peerdir = f"PEERDIR({peers})\n" if peers else ""
        files[f"l{i}/ya.make"] = (
            f"LIBRARY()\n{BARE_MODULE}{peerdir}ADDINCL(GLOBAL l{i}/inc)\nSRCS(x.cpp)\nEND()\n"
        )
        files[f"l{i}/x.cpp"] = "int x(){return 0;}\n"
        files[f"l{i}/inc/.keep"] = ""
    files["app/ya.make"] = (
        f"PROGRAM()\n{BARE_MODULE}PEERDIR({' '.join(f'l{i}' for i in range(count))})\n"
        "SRCS(main.cpp)\nEND()\n"
    )
    files["app/main.cpp"] = "int main(){return 0;}\n"
    return files


def bucketed(inputs, fixed):
    """Inputs with their first `fixed` entries in order and the rest sorted.

    A node lists its include closure after its own inputs, laid out in buckets
    of interned path ids, so the closure order moves with interning order; the
    validation gate compares inputs sorted, and so do the tests.
    """
    return list(inputs[:fixed]) + sorted(inputs[fixed:])


def resource_lib(name, sbr):
    return f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({name} sbr:{sbr})\nEND()\n"


def java_tree(**extra):
    files = {
        "build/platform/python/ymake_python3/ya.make": resource_lib("YMAKE_PYTHON3", 1),
        "build/platform/java/jdk/ya.make": resource_lib("JDK_DEFAULT", 2),
        "build/platform/java/jdk/jdk17/ya.make": resource_lib("JDK17", 3),
        "build/platform/java/jdk/jdk26/ya.make": resource_lib("JDK26", 5),
        "build/platform/java/protoc/ya.make": resource_lib("JAVA_PROTOC", 4),
    }
    files.update(extra)
    return files
