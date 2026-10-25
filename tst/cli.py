import os
import subprocess

from tst import lib


ESC = "\x1b"
RED = ESC + "[31m"
RESET = ESC + "[0m"
USAGE_HEAD = ESC + "[92musage:" + RESET + " ay [global-flags] "


def run_ay(*args, env=None, cwd=None, timeout=30):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        env=env,
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


def fatal(message):
    return RED + message + RESET + "\n"


def toolchain_free_env(**extra):
    env = {
        key: value
        for key, value in os.environ.items()
        if key not in lib.TOOLCHAIN_ENV_VARS
    }
    env.update(extra)
    return env
