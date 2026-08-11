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


def run(*args, timeout=10, env=None):
    command = [str(AY), *map(str, args)]
    result = subprocess.run(
        command,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )
    if result.returncode != 0:
        raise AssertionError(
            f"command failed with exit code {result.returncode}: {command!r}\n"
            f"--- stdout ---\n{result.stdout}"
            f"--- stderr ---\n{result.stderr}"
        )
    return result


def make(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-make-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in TOOLCHAIN_ENV_VARS
        }
        result = run(
            "make", "-j0", "-G", "--sandboxing",
            "--source-root", root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
            *args,
            target,
            env=env,
        )
        return json.loads(result.stdout)
