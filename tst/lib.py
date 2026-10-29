import os
import subprocess
from pathlib import Path


AY = Path(os.environ["AY_TEST_BINARY"])
NORMALIZE = Path(os.environ["AY_NORMALIZE_BINARY"])
SORT = Path(os.environ["AY_SORT_BINARY"])
TOOLS = {"normalize": NORMALIZE, "sort": SORT}


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


def run_norm(tool, *args, timeout=10):
    return run(*args, timeout=timeout, binary=TOOLS[tool])
