import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def fixture():
    files = {
        "nomod/ya.make": "SRCS(a.cpp)\n",
        "ok/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}SRCS(ok.cpp GLOBAL g.cpp x.c)\n"
            "LD_PLUGIN(plug.py)\nEND()\n"
        ),
        "ok/ok.cpp": "int ok;\n",
        "ok/g.cpp": "int g;\n",
        "ok/x.c": "int x;\n",
        "ok/plug.py": "\n",
        "dyn/ya.make": (
            f"DYNAMIC_LIBRARY(d)\n{NO_PLATFORM}EXPORTS_SCRIPT(d.exports)\n"
            "DYNAMIC_LIBRARY_FROM(ok)\nEND()\n"
        ),
        "dyn/d.exports": "{};\n",
        "build/platform/local_so/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\n",
        "app/ya.make": (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(nomod ok dyn)\nEND()\n"
        ),
        "app/m.cpp": "int main(){return 0;}\n",
    }
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files


def generate(files, extra_env):
    with tempfile.TemporaryDirectory(prefix="ay-link-ownership-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS and key != "AY_DEBUG_OWNERSHIP"
        }
        env.update(extra_env)
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing", "-k",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                "app",
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


class OwnershipDebugTest(unittest.TestCase):
    def test_arena_ownership_audit_reports_no_violations(self):
        plain = generate(fixture(), {})
        audited = generate(fixture(), {"AY_DEBUG_OWNERSHIP": "1"})
        self.assertEqual(plain.returncode, 0, plain.stderr)
        self.assertEqual(audited.returncode, 0, audited.stderr)
        failure = "module-failed: nomod: gen: nomod has no module declaration"
        self.assertIn(failure, plain.stderr)
        self.assertIn(failure, audited.stderr)
        self.assertNotIn("ownership:", plain.stderr)
        self.assertIn("ownership: 0 violating (field, site) pairs\n", audited.stderr)
        self.assertEqual(json.loads(audited.stdout), json.loads(plain.stdout))
        link = lib.node_by_output(json.loads(audited.stdout), "$(B)/app/app")
        self.assertIn("$(B)/ok/libok.a", link["inputs"])
        self.assertIn("$(B)/dyn/libd.so", link["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
