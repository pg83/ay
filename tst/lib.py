import json
import os
import subprocess
import tempfile
from pathlib import Path


AY = Path(os.environ["AY_TEST_BINARY"])


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
        (root / ".arcadia.root").touch()
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
