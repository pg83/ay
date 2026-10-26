import os
import subprocess
from pathlib import Path


AY = Path(os.environ["AY_TEST_BINARY"])
NORM = Path(os.environ["AY_NORM_BINARY"])


def run_process(*args, timeout=10, env=None, binary=AY):
    return subprocess.run(
        [str(binary), *map(str, args)],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


def run(*args, timeout=10, env=None, binary=AY):
    result = run_process(*args, timeout=timeout, env=env, binary=binary)
    if result.returncode != 0:
        raise AssertionError(
            f"command failed with exit code {result.returncode}: {result.args!r}\n"
            f"--- stdout ---\n{result.stdout}"
            f"--- stderr ---\n{result.stderr}"
        )
    return result


def run_norm(*args, timeout=10):
    return run(*args, timeout=timeout, binary=NORM)
