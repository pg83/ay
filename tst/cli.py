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
