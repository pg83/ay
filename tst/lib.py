import os
import subprocess
from pathlib import Path


AY = Path(os.environ["AY_TEST_BINARY"])


def run(*args, timeout=10):
    command = [str(AY), *map(str, args)]
    result = subprocess.run(
        command,
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
