import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


INCLUDERS = {
    "A": "contrib/libs/foo/a.cpp",
    "B": "util/b.cpp",
    "C": "other/c.cpp",
    "D": "contrib/tools/d.cpp",
}

# (probe header, source_filter as written in YAML, includers it claims for).
SOURCE_FILTERS = [
    ("literal_prefix", "^contrib/libs/foo", "A"),
    ("contains", ".*libs.*", "A"),
    ("literal_alternatives", "^(contrib/libs/foo|util)", "AB"),
    ("lookahead_only", "^(?!contrib/libs/foo)", "BCD"),
    ("lookahead_group_any", "^(?!(contrib/libs|util)).*", "CD"),
    ("lookahead_then_literal", "^(?!contrib)util", "B"),
    ("lookahead_then_regex", "^(?!contrib/libs/foo)(util|other)", "BC"),
    ("lookahead_empty_alternative", "^(?!contrib/libs/foo||util)", "CD"),
    ("prefixed_lookahead", "^contrib(?!/libs/foo)", "D"),
    ("prefixed_lookahead_any", "^contrib(?!/libs/foo).*", "D"),
    ("nested_alternatives", "^contrib/(libs/(foo|bar)|tools)", "AD"),
    ("empty_alternative", "^(ab|)contrib/tools", "D"),
    ("guarded_regex", "^contrib/libs/fo.", "A"),
    ("char_class_regex", "^util/(b|c)[.]cpp", "B"),
    ("fold_case", "^(?i)CONTRIB/LIBS/FOO", "A"),
    ("top_level_overflow", "^(contrib|util)/(aa|bb|cc|dd|ee|ff|gg|hh|ii)(aa|bb|cc|dd|ee|ff|gg|hh|ii)", ""),
    ("concat_overflow", "^((aa|bb|cc|dd|ee|ff|gg|hh|ii)(aa|bb|cc|dd|ee|ff|gg|hh|ii))", ""),
    ("alternate_overflow", "^((aa|bb|cc|dd|ee|ff|gg|hh)(aa|bb|cc|dd|ee|ff|gg|hh)|zz)", ""),
    ("bracketed_bar", "^[|]x|^util", "B"),
    ("escaped_dot", "^util\\\\.x|^contrib/libs/foo", "A"),
    ("empty_filter", "", "ABCD"),
    ("anchor_only", "^", "ABCD"),
    ("alternative_with_regex", "^(contrib/libs/foo|u.il)", "AB"),
    ("lookahead_then_anchor", "^(?!contrib)^util", "B"),
    ("contains_regex", ".*lib./foo.*", "A"),
    ("unanchored_regex", "[/]b[.]c(c|pp)$", "B"),
]

# Filters the loader rejects: the record is disabled with a sysincl warning.
UNSUPPORTED_FILTERS = [
    ("residual_after_prefixed", "^contrib(?!/libs/foo)/tools"),
    ("two_lookaheads", "^(?!contrib)(?!util).*"),
    ("unanchored_lookahead", "util(?!/b)"),
    ("unclosed_lookahead", "^(?!contrib"),
    ("regex_in_lookahead", "^(?!contrib.*)"),
    ("bad_regex", "^contrib/(unclosed"),
    ("meta_before_lookahead", "^con.rib(?!/libs)"),
    ("escape_in_lookahead", "^(?!util\\\\)x)"),
]


def make_process(files, target, *args, opensource=True):
    with tempfile.TemporaryDirectory(prefix="ay-include-sysincl-") as directory:
        root = Path(directory)
        lib.seed_root(root)
        value = 'OPENSOURCE = "yes"\n' if opensource else ""
        (root / "ya.conf").write_text(
            f"[flags]\n{value}\n[host_platform_flags]\n{value}"
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


def module(sources):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"SRCS({sources})\nEND()\n"
    )


def unresolved(stderr):
    return set(re.findall(r"\$\(S\)/(\S+): unresolved include [<\"](.+?)[>\"] ", stderr))


def succeed(files, target, *args, opensource=True):
    result = make_process(files, target, "-k", *args, opensource=opensource)
    if result.returncode != 0:
        raise AssertionError(f"make failed:\n{result.stderr}")
    return json.loads(result.stdout), result.stderr


class SysinclSourceFilterTest(unittest.TestCase):
    def test_source_filter_forms(self):
        records = []
        probes = []
        for name, pattern, _ in SOURCE_FILTERS:
            records.append(f'- source_filter: "{pattern}"\n  includes:\n  - {name}.h\n')
            probes.append(name)
        for name, pattern in UNSUPPORTED_FILTERS:
            records.append(f'- source_filter: "{pattern}"\n  includes:\n  - {name}.h\n')
            probes.append(name)
        source = "".join(f"#include <{name}.h>\n" for name in probes)
        files = {"all/ya.make": module(" ".join(INCLUDERS.values()))}
        files.update({path: source for path in INCLUDERS.values()})
        files["build/sysincl/misc.yml"] = "".join(records)
        _, stderr = succeed(files, "all", "--verbose")
        missing = unresolved(stderr)
        for name, pattern, claimed in SOURCE_FILTERS:
            for key, path in INCLUDERS.items():
                with self.subTest(filter=pattern, includer=path):
                    self.assertEqual(key not in claimed, (path, f"{name}.h") in missing)
            self.assertNotIn(f'source_filter "{pattern}" unsupported', stderr)
        for name, pattern in UNSUPPORTED_FILTERS:
            with self.subTest(filter=pattern):
                self.assertIn(f'source_filter "{pattern}" unsupported', stderr)
                self.assertEqual(
                    set(INCLUDERS.values()),
                    {path for path, header in missing if header == f"{name}.h"},
                )


class SysinclCythonTest(unittest.TestCase):
    def test_cython_set_follows_use_python3_prev(self):
        files = {
            "m/ya.make": module("a.cpp"),
            "m/a.cpp": "#include <Python.h>\n",
            "build/conf/sysincl.conf": (
                "SYSINCL=\n"
                'when ($USE_PYTHON3_PREV == "yes") {\n'
                "    SYSINCL+=build/sysincl/cython-prev.yml\n"
                "}\n"
                "otherwise {\n"
                "    SYSINCL+=build/sysincl/cython.yml\n"
                "}\n"
            ),
            "build/sysincl/cython.yml": "- includes:\n  - Python.h: contrib/libs/python/Include/Python.h\n",
            "build/sysincl/cython-prev.yml": "- includes:\n  - Python.h: contrib/libs/python/Include_prev/Python.h\n",
            "contrib/libs/python/Include/Python.h": "",
            "contrib/libs/python/Include_prev/Python.h": "",
        }
        for flags, header in (((), "Include"), (("-DUSE_PYTHON3_PREV=yes",), "Include_prev")):
            with self.subTest(header=header):
                graph, stderr = succeed(files, "m", *flags)
                self.assertEqual(set(), unresolved(stderr))
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertIn(f"$(S)/contrib/libs/python/{header}/Python.h", inputs)
                self.assertEqual(1, sum(1 for i in inputs if i.endswith("/Python.h")))


class SysinclMappingTest(unittest.TestCase):
    CONFIG = (
        "# leading comment\n"
        "- includes:\n"
        "  - claimed.h\n"
        "  - mapped.h: contrib/libs/mapped/mapped.h   # trailing comment\n"
        "  - absent.h: contrib/libs/absent/absent.h\n"
        '  - "colon:key.h": "contrib/libs/quoted/q.h"\n'
        '  - "esc\\"aped.h": contrib/libs/quoted/escaped.h\n'
        '  - empty.h: ""\n'
        "  - multi.h:\n"
        "    - contrib/libs/multi/one.h\n"
        '    - ""\n'
        "    - contrib/libs/multi/two.h\n"
        "  - after_multi.h: contrib/libs/mapped/after.h\n"
        "  - repeated_target.h:\n"
        "    - contrib/libs/mapped/mapped.h\n"
        "    - contrib/libs/mapped/mapped.h\n"
        "- includes: []\n"
        "- includes:\n"
        "  - dup.h: contrib/libs/dup/first.h\n"
        "  - dup.h: contrib/libs/dup/second.h\n"
        "  - Case.h: contrib/libs/case/upper.h\n"
        "  - case.h: contrib/libs/case/lower.h\n"
        "  - local.h: contrib/libs/local/local.h\n"
        "  - sibling_multi.h:\n"
        "    - contrib/libs/sm/one.h\n"
        "    - contrib/libs/sm/two.h\n"
        "  - rooted_multi.h:\n"
        "    - contrib/libs/rm/one.h\n"
        "    - contrib/libs/rm/two.h\n"
        "  - angle_found.h: contrib/libs/af/af.h\n"
        "  - angle_same.h: angle_same.h\n"
        "  - angle_found_unmapped.h: contrib/libs/nope/nope.h\n"
        "  - merged.h: contrib/libs/merge/one.h\n"
        "- includes:\n"
        "  - merged.h:\n"
        "    - contrib/libs/merge/two.h\n"
        "    - contrib/libs/merge/three.h\n"
        '- source_filter: "^m/"\n'
        "  includes:\n"
        "  - shared.h: contrib/libs/shared/one.h\n"
        "- includes:\n"
        "  - shared.h: contrib/libs/shared/two.h\n"
        "  - root_sibling.h:\n"
        "    - contrib/libs/rs/one.h\n"
        "    - contrib/libs/rs/two.h\n"
    )
    TARGETS = [
        "contrib/libs/mapped/mapped.h", "contrib/libs/quoted/q.h",
        "contrib/libs/quoted/escaped.h", "contrib/libs/multi/one.h",
        "contrib/libs/multi/two.h", "contrib/libs/mapped/after.h",
        "contrib/libs/dup/first.h", "contrib/libs/dup/second.h",
        "contrib/libs/case/upper.h", "contrib/libs/case/lower.h",
        "contrib/libs/local/local.h", "contrib/libs/sm/one.h",
        "contrib/libs/sm/two.h", "contrib/libs/rm/one.h",
        "contrib/libs/rm/two.h", "contrib/libs/af/af.h",
        "contrib/libs/shared/one.h", "contrib/libs/shared/two.h",
        "contrib/libs/rs/one.h", "contrib/libs/rs/two.h",
        "contrib/libs/merge/one.h", "contrib/libs/merge/two.h",
        "contrib/libs/merge/three.h",
    ]

    def test_claims_and_mappings(self):
        files = {
            "m/ya.make": module("a.cpp root.cpp"),
            "m/a.cpp": (
                "#include <claimed.h>\n"
                "#include <mapped.h>\n"
                "#include <absent.h>\n"
                "#include <colon:key.h>\n"
                '#include <esc"aped.h>\n'
                "#include <empty.h>\n"
                "#include <multi.h>\n"
                "#include <after_multi.h>\n"
                "#include <repeated_target.h>\n"
                "#include <dup.h>\n"
                "#include <Case.h>\n"
                "#include <case.h>\n"
                "#include <shared.h>\n"
                '#include "local.h"\n'
                '#include "sibling_multi.h"\n'
                '#include "rooted_multi.h"\n'
                "#include <angle_found.h>\n"
                "#include <angle_same.h>\n"
                "#include <angle_found_unmapped.h>\n"
                "#include <merged.h>\n"
            ),
            "m/local.h": "",
            "m/sibling_multi.h": "",
            "rooted_multi.h": "",
            "angle_found.h": "",
            "angle_same.h": "",
            "angle_found_unmapped.h": "",
            "root.cpp": '#include "root_sibling.h"\n',
            "root_sibling.h": "",
            "build/sysincl/misc.yml": self.CONFIG,
        }
        for target in self.TARGETS:
            files[target] = ""
        graph, stderr = succeed(files, "m")
        self.assertEqual(set(), unresolved(stderr))
        inputs = set(lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"])
        expected = {
            "$(S)/m/a.cpp",
            "$(S)/contrib/libs/mapped/mapped.h",
            "$(S)/contrib/libs/quoted/q.h",
            "$(S)/contrib/libs/quoted/escaped.h",
            "$(S)/contrib/libs/multi/one.h",
            "$(S)/contrib/libs/multi/two.h",
            "$(S)/contrib/libs/mapped/after.h",
            # Within one record the last duplicate key wins.
            "$(S)/contrib/libs/dup/second.h",
            "$(S)/contrib/libs/case/upper.h",
            "$(S)/contrib/libs/case/lower.h",
            # Every record whose filter matches contributes its targets.
            "$(S)/contrib/libs/shared/one.h",
            "$(S)/contrib/libs/shared/two.h",
            # A quoted include found next to its includer bypasses sysincl.
            "$(S)/m/local.h",
            "$(S)/m/sibling_multi.h",
            # Found elsewhere, a multi-target mapping is merged in.
            "$(S)/rooted_multi.h",
            "$(S)/contrib/libs/rm/one.h",
            "$(S)/contrib/libs/rm/two.h",
            # Angle includes keep both the search result and the mapping.
            "$(S)/angle_found.h",
            "$(S)/contrib/libs/af/af.h",
            "$(S)/angle_same.h",
            # A mapping to a missing file adds nothing.
            "$(S)/angle_found_unmapped.h",
            "$(S)/contrib/libs/merge/one.h",
            "$(S)/contrib/libs/merge/two.h",
            "$(S)/contrib/libs/merge/three.h",
        }
        self.assertEqual(expected, inputs)
        root_inputs = set(lib.node_by_output(graph, "$(B)/m/__/root.cpp.o")["inputs"])
        self.assertEqual({"$(S)/root.cpp", "$(S)/root_sibling.h"}, root_inputs)

    def test_case_insensitive_records(self):
        files = {
            "m/ya.make": module("a.cpp b.cpp"),
            "m/a.cpp": (
                "#include <windows.h>\n"
                "#include <WINDOWS.H>\n"
                "#include <winsock2.h>\n"
                "#include <X>\n"
                "#include <_CFG.h>\n"
                "#include <Foo.h>\n"
                "#include <foo.h>\n"
                "#include <wINDOWSx.h>\n"
                "#include <Wz>\n"
                "#include <a_name_longer_than_every_case_insensitive_key.h>\n"
            ),
            "m/b.cpp": "#include <Windows.h>\n#include <windows.h>\n",
            "contrib/win/windows.h": "",
            "contrib/foo/cs.h": "",
            "contrib/foo/ci.h": "",
            "build/sysincl/windows.yml": (
                "- case_sensitive: false\n"
                "  includes:\n"
                "  - Windows.h: contrib/win/windows.h\n"
                "  - WinSock2.h\n"
                "  - x\n"
                "  - _cfg.h\n"
                "  - FOO.H: contrib/foo/ci.h\n"
                "- case_sensitive: true\n"
                "  includes:\n"
                "  - Foo.h: contrib/foo/cs.h\n"
            ),
        }
        graph, stderr = succeed(files, "m")
        self.assertEqual(
            {
                ("m/a.cpp", "wINDOWSx.h"),
                ("m/a.cpp", "Wz"),
                ("m/a.cpp", "a_name_longer_than_every_case_insensitive_key.h"),
            },
            unresolved(stderr),
        )
        a_inputs = set(lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"])
        self.assertEqual(
            {
                "$(S)/m/a.cpp", "$(S)/contrib/win/windows.h",
                "$(S)/contrib/foo/cs.h", "$(S)/contrib/foo/ci.h",
            },
            a_inputs,
        )
        b_inputs = set(lib.node_by_output(graph, "$(B)/m/b.cpp.o")["inputs"])
        self.assertEqual({"$(S)/m/b.cpp", "$(S)/contrib/win/windows.h"}, b_inputs)


class SysinclYamlSubsetTest(unittest.TestCase):
    def test_accepted_syntax(self):
        config = (
            "includes:\n"
            "  - top_level.h\n"
            "\t\n"
            "\t# tab comment\n"
            "- includes:\r\n"
            "    - crlf.h\r\n"
            "    - hash#inside.h\n"
            '    - "quoted # hash.h"\n'
            '    - "back\\\\slash.h"\n'
            "- case_sensitive: true\n"
            "  source_filter: \"^m/\"\n"
            "  includes:\n"
            "      - deep.h\n"
            "- includes:\n"
            "  - last.h"
        )
        files = {
            "m/ya.make": module("a.cpp"),
            "m/a.cpp": (
                "#include <top_level.h>\n"
                "#include <crlf.h>\n"
                "#include <hash#inside.h>\n"
                "#include <quoted # hash.h>\n"
                "#include <back\\slash.h>\n"
                "#include <deep.h>\n"
                "#include <last.h>\n"
            ),
            "build/sysincl/misc.yml": config,
        }
        _, stderr = succeed(files, "m")
        self.assertEqual(set(), unresolved(stderr))

    def test_unrecognised_key_disables_record(self):
        files = {
            "m/ya.make": module("a.cpp"),
            "m/a.cpp": "#include <disabled.h>\n#include <enabled.h>\n",
            "build/sysincl/misc.yml": (
                "- bogus: 1\n"
                "  includes:\n"
                "  - disabled.h\n"
                "- includes:\n"
                "  - enabled.h\n"
            ),
        }
        _, stderr = succeed(files, "m", "--verbose")
        self.assertIn('misc.yml:1: unrecognised record key "bogus"', stderr)
        self.assertEqual({("m/a.cpp", "disabled.h")}, unresolved(stderr))

    def test_rejected_syntax(self):
        cases = {
            "\t- x.h\n": "misc.yml:1: tab indentation is not part of the sysincl subset",
            "-\n": "misc.yml:1: empty sequence item",
            "  - x.h\n": "misc.yml:1: sequence item outside a record",
            "  includes:\n": 'misc.yml:1: unsupported top-level line "includes:"',
            "- includes: [x.h]\n": "misc.yml:1: inline includes value is not part of the sysincl subset",
            "- includes\n": "misc.yml:1: expected `key: value`",
            '- includes:\n  - "x.h\n': "misc.yml:2: unterminated quoted scalar",
            '- includes:\n  - "x\\n.h"\n': "misc.yml:2: escape \\n is not part of the sysincl subset",
            '- includes:\n  - "x.h\\"\n': "misc.yml:2: dangling escape",
        }
        for config, message in cases.items():
            with self.subTest(config=config):
                files = {
                    "m/ya.make": module("a.cpp"),
                    "m/a.cpp": "#include <x.h>\n",
                    "build/sysincl/misc.yml": config,
                }
                result = make_process(files, "m")
                self.assertEqual(1, result.returncode)
                self.assertIn(message, result.stderr)


# build/conf/sysincl.conf, the internal configuration ymake_conf.py imports and
# a project conf, each listing sysincl files as the upstream ones do.
SYSINCL_CONF = """SYSINCL=
SYSINCL+=build/sysincl/misc.yml

when ($OPENSOURCE == "yes") {
    SYSINCL+=build/sysincl/opensource.yml
}
otherwise {
    SYSINCL+=build/sysincl/proto.yml
}

when ($MUSL == "yes") {
    SYSINCL+=build/sysincl/libc-to-musl.yml
    when ($ARCH_AARCH64 == "yes") {
        SYSINCL+=build/sysincl/linux-musl-aarch64.yml
    }
    otherwise {
        SYSINCL+=build/sysincl/linux-musl.yml
    }
}
"""

INTERNAL_CONF = """@import "${CONF_ROOT}/internal/conf/sysincl.conf"

@import "${CONF_ROOT}/internal/conf/project_specific/smart_devices.conf"
"""

INTERNAL_SYSINCL_CONF = """when ($OPENSOURCE != "yes") {
    SYSINCL+=build/internal/sysincl/misc.yml
}

when ($OS_WINDOWS == "yes") {
    when ($OPENSOURCE !="yes") {
        SYSINCL+=build/internal/sysincl/misc-win.yml
    }
}

when ($USE_STL_SYSTEM != "yes" && $NORUNTIME != "yes" && $OPENSOURCE != "yes") {
    SYSINCL+=build/internal/sysincl/taxi.yml
}
"""

SMART_DEVICES_CONF = """when ($OS_LINUX == "yes") {
    SYSINCL+=build/internal/sysincl/smart_devices_linux.yml
}
elsewhen ($OS_ANDROID == "yes") {
    SYSINCL+=build/internal/sysincl/smart_devices_android.yml
}
elsewhen ($OS_DARWIN == "yes") {
    SYSINCL+=build/internal/sysincl/smart_devices_darwin.yml
}
"""


def claims(probes, files, *args, opensource=True):
    """The probe headers the sysincl configuration of the tree claims."""
    files = {
        "m/ya.make": module("a.cpp"),
        "m/a.cpp": "".join(f"#include <{probe}.h>\n" for probe in probes),
        **files,
    }
    _, stderr = succeed(files, "m", *args, opensource=opensource)
    missing = {header[:-len(".h")] for _, header in unresolved(stderr)}
    return set(probes) - missing


class SysinclFileSelectionTest(unittest.TestCase):
    FILES = {
        "build/conf/sysincl.conf": SYSINCL_CONF,
        "build/internal/conf/internal.conf": INTERNAL_CONF,
        "build/internal/conf/sysincl.conf": INTERNAL_SYSINCL_CONF,
        "build/internal/conf/project_specific/smart_devices.conf": SMART_DEVICES_CONF,
        "build/sysincl/misc.yml": "- includes:\n  - misc.h\n",
        "build/sysincl/opensource.yml": "- includes:\n  - opensource.h\n",
        "build/sysincl/proto.yml": "- includes:\n  - proto.h\n",
        "build/sysincl/libc-to-musl.yml": "- includes:\n  - musl.h\n",
        "build/sysincl/linux-musl-aarch64.yml": "- includes:\n  - musl_aarch64.h\n",
        "build/sysincl/linux-musl.yml": "- includes:\n  - musl_x86_64.h\n",
        "build/internal/sysincl/misc.yml": "- includes:\n  - internal.h\n",
        "build/internal/sysincl/misc-win.yml": "- includes:\n  - windows.h\n",
        "build/internal/sysincl/smart_devices_darwin.yml": "- includes:\n  - darwin.h\n",
        "build/internal/sysincl/smart_devices_linux.yml": "- includes:\n  - linux.h\n",
        "build/internal/sysincl/taxi.yml": "- includes:\n  - taxi.h\n",
        "build/internal/sysincl/qt.yml": "- includes:\n  - qt.h\n",
    }
    PROBES = [
        "misc", "opensource", "proto", "musl", "musl_aarch64", "musl_x86_64",
        "internal", "windows", "darwin", "linux", "taxi", "qt",
    ]

    def claimed(self, *args, opensource=True, internal=True):
        files = {
            path: content
            for path, content in self.FILES.items()
            if internal or not path.startswith("build/internal/")
        }
        return claims(self.PROBES, files, *args, opensource=opensource)

    def test_opensource_linux(self):
        self.assertEqual({"misc", "opensource"}, self.claimed(internal=False))
        # ymake_conf.py imports the internal configuration whenever it exists;
        # only the conditions inside it look at OPENSOURCE.
        self.assertEqual({"misc", "opensource", "linux"}, self.claimed())

    def test_opensource_musl_aarch64(self):
        self.assertEqual(
            {"misc", "opensource", "musl", "musl_aarch64"},
            self.claimed("--musl", internal=False),
        )

    def test_internal_musl_x86_64(self):
        self.assertEqual(
            {"misc", "proto", "musl", "musl_x86_64", "internal", "linux", "taxi"},
            self.claimed(
                "--musl", "--target-platform", "default-linux-x86_64",
                opensource=False,
            ),
        )

    def test_internal_windows_and_darwin(self):
        self.assertEqual(
            {"misc", "proto", "internal", "windows", "taxi"},
            self.claimed("--target-platform", "default-windows-x86_64", opensource=False),
        )
        self.assertEqual(
            {"misc", "proto", "internal", "darwin", "taxi"},
            self.claimed("--target-platform", "default-darwin-x86_64", opensource=False),
        )

    def test_internal_configuration_is_optional(self):
        self.assertEqual({"misc", "proto"}, self.claimed(opensource=False, internal=False))


class SysinclConfTest(unittest.TestCase):
    YML = {
        "build/sysincl/misc.yml": "- includes:\n  - misc.h\n",
        "build/sysincl/zephyr.yml": (
            '- source_filter: "^m/a[.]cpp"\n'
            "  includes:\n"
            "  - picolibc.h\n"
        ),
    }

    def test_files_are_the_ones_sysincl_conf_lists(self):
        older = "SYSINCL=\nSYSINCL+=build/sysincl/misc.yml\n"
        newer = older + "SYSINCL+=build/sysincl/zephyr.yml\n"
        for conf, claimed in ((older, {"misc"}), (newer, {"misc", "picolibc"})):
            with self.subTest(conf=conf):
                files = {**self.YML, "build/conf/sysincl.conf": conf}
                self.assertEqual(claimed, claims(["misc", "picolibc"], files))

    def test_every_imported_conf_contributes(self):
        files = {
            **self.YML,
            "build/ymake.core.conf": (
                '@import "${CONF_ROOT}/conf/sysincl.conf"\n'
                "when ($USE_STL_SYSTEM != \"yes\") {\n"
                "    when ($NORUNTIME != \"yes\") {\n"
                "        SYSINCL+=build/sysincl/zephyr.yml\n"
                "    }\n"
                "}\n"
                '@import "${CONF_ROOT}/conf/python.conf"\n'
            ),
            "build/conf/sysincl.conf": "SYSINCL=\n",
            "build/conf/python.conf": (
                "macro _NOT_A_CONF_STATEMENT() {\n"
                "    SYSINCL+=build/sysincl/never.yml\n"
                "}\n"
                "CONTINUED = \\\n"
                "  a \\\n"
                "\n"
                "when ($OPENSOURCE) {\n"
                "    SYSINCL+=build/sysincl/misc.yml\n"
                "}\n"
            ),
            "build/sysincl/never.yml": "- includes:\n  - never.h\n",
        }
        self.assertEqual({"misc", "picolibc"}, claims(["misc", "picolibc", "never"], files))
        self.assertEqual({"picolibc"}, claims(["misc", "picolibc", "never"], files, opensource=False))

    def test_conditional_assignments_apply_after_the_unconditional_ones(self):
        # ymake applies the unconditional assignments while reading the conf
        # and every conditional one afterwards, in order: the reset inside
        # `when` drops misc.yml although misc.yml is appended after it.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": (
                "SYSINCL=\n"
                'when ($OS_LINUX == "yes") {\n'
                "    SYSINCL=build/sysincl/zephyr.yml\n"
                "}\n"
                "SYSINCL+=build/sysincl/misc.yml\n"
            ),
        }
        self.assertEqual({"picolibc"}, claims(["misc", "picolibc"], files))
        windows = ("--target-platform", "default-windows-x86_64")
        self.assertEqual({"misc"}, claims(["misc", "picolibc"], files, *windows))

    def test_entries_expand_platform_variables(self):
        # conf.cpp LoadSystemHeaders takes SubstVarDeeply(SYSINCL): a -D
        # variable names a file, an unset one expands to nothing.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": "SYSINCL=build/sysincl/misc.yml\nSYSINCL+=${EXTRA_SYSINCL}\n",
        }
        probes = ["misc", "picolibc"]
        self.assertEqual({"misc"}, claims(probes, files))
        extra = ("-DEXTRA_SYSINCL=build/sysincl/zephyr.yml",)
        self.assertEqual({"misc", "picolibc"}, claims(probes, files, *extra))

    def test_conditions_see_earlier_conditional_assignments(self):
        # TCondition::RecalcAll applies the conditional assignments one by
        # one, each condition evaluated against the variables as they are
        # at that moment.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": (
                "SYSINCL=\n"
                'when ($OS_LINUX == "yes") {\n'
                "    SYSINCL=build/sysincl/zephyr.yml\n"
                "}\n"
                'when ($SYSINCL == "build/sysincl/zephyr.yml") {\n'
                "    SYSINCL+=build/sysincl/misc.yml\n"
                "}\n"
            ),
        }
        self.assertEqual({"misc", "picolibc"}, claims(["misc", "picolibc"], files))

    def test_conf_variables_feed_the_list(self):
        # SubstVarDeeply(SYSINCL) expands the variables the configuration
        # itself assigns, in any imported file, under any condition.
        probes = ["misc", "picolibc"]
        for conf, core in (
            ("ZEPHYR_SYSINCL=build/sysincl/zephyr.yml\nSYSINCL=build/sysincl/misc.yml $ZEPHYR_SYSINCL\n", ""),
            ("SYSINCL=build/sysincl/misc.yml ${ZEPHYR_SYSINCL}\n", "ZEPHYR_SYSINCL=build/sysincl/zephyr.yml\n"),
            (
                "SYSINCL=build/sysincl/misc.yml\nSYSINCL+=$ZEPHYR_SYSINCL\n",
                'when ($OS_LINUX == "yes") {\n    ZEPHYR_SYSINCL=build/sysincl/zephyr.yml\n}\n',
            ),
        ):
            with self.subTest(conf=conf, core=core):
                files = {
                    **self.YML,
                    "build/ymake.core.conf": core + '@import "${CONF_ROOT}/conf/sysincl.conf"\n',
                    "build/conf/sysincl.conf": conf,
                }
                self.assertEqual({"misc", "picolibc"}, claims(probes, files))

    def test_conditions_read_conf_variables(self):
        # The unconditional default first, then the conditional assignments
        # in order: the second condition sees USE_ZEPHYR=yes set by the first.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": (
                "SYSINCL=build/sysincl/misc.yml\n"
                "USE_ZEPHYR=no\n"
                'when ($OS_LINUX == "yes") {\n'
                "    USE_ZEPHYR=yes\n"
                "}\n"
                'when ($USE_ZEPHYR == "yes") {\n'
                "    SYSINCL+=build/sysincl/zephyr.yml\n"
                "}\n"
            ),
        }
        probes = ["misc", "picolibc"]
        self.assertEqual({"misc", "picolibc"}, claims(probes, files))
        windows = ("--target-platform", "default-windows-x86_64")
        self.assertEqual({"misc"}, claims(probes, files, *windows))

    def test_command_line_overrides_conf_defaults(self):
        # ymake_conf.py prints the -D presets after importing the conf, so
        # they replace the unconditional defaults of the conf files.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": "EXTRA_SYSINCL=build/sysincl/misc.yml\nSYSINCL=$EXTRA_SYSINCL\n",
        }
        probes = ["misc", "picolibc"]
        self.assertEqual({"misc"}, claims(probes, files))
        extra = ("-DEXTRA_SYSINCL=build/sysincl/zephyr.yml",)
        self.assertEqual({"picolibc"}, claims(probes, files, *extra))

    def test_define_and_exclude_operators(self):
        # vars.h DelFromSingleVal: `-=` removes substrings from the value,
        # `:=` sets a variable only while it is undefined.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": (
                "SYSINCL=build/sysincl/zephyr.yml\n"
                "SYSINCL-=build/sysincl/zephyr.yml\n"
                "EXTRA_SYSINCL:=build/sysincl/misc.yml\n"
                "EXTRA_SYSINCL:=build/sysincl/zephyr.yml\n"
                "SYSINCL+=$EXTRA_SYSINCL\n"
            ),
        }
        self.assertEqual({"misc"}, claims(["misc", "picolibc"], files))

    def test_exclude_removes_substrings_until_no_match_remains(self):
        variants = (
            "EXTRA_SYSINCL=build/sysincl/notfirst.yml\n"
            "EXTRA_SYSINCL-=not\nSYSINCL=$EXTRA_SYSINCL\n",
            "DROP=aabcbc\nDROP-=abc\n"
            "SYSINCL=build/sysincl/${DROP}first.yml\n",
        )
        for conf in variants:
            with self.subTest(conf=conf):
                files = {
                    **self.YML,
                    "build/sysincl/first.yml": self.YML["build/sysincl/misc.yml"],
                    "build/conf/sysincl.conf": conf,
                }
                self.assertEqual({"misc"}, claims(["misc", "picolibc"], files))

    def test_select_alternatives(self):
        files = {
            **self.YML,
            "build/conf/sysincl.conf": (
                "SYSINCL=\n"
                "select ($HARDWARE_TYPE) {\n"
                '    "x86_64" | "aarch64" ? {\n'
                "        SYSINCL+=build/sysincl/zephyr.yml\n"
                "    }\n"
                "    default ? {\n"
                "        SYSINCL+=build/sysincl/misc.yml\n"
                "    }\n"
                "}\n"
            ),
        }
        self.assertEqual({"picolibc"}, claims(["misc", "picolibc"], files))

    def test_ymake_conf_output_takes_part(self):
        # The conf ymake reads is the ymake_conf.py output with its imports
        # inlined; a condition printed by ymake_conf.py feeds SYSINCL too.
        files = {
            **self.YML,
            "build/conf/sysincl.conf": "SYSINCL=build/sysincl/misc.yml $ZEPHYR_SYSINCL\n",
            "build/ymake_conf.py": lib.ymake_conf(
                extra='when ($OS_LINUX == "yes") {\n    ZEPHYR_SYSINCL=build/sysincl/zephyr.yml\n}\n',
            ),
        }
        self.assertEqual({"misc", "picolibc"}, claims(["misc", "picolibc"], files))

    def test_import_loop_is_fatal(self):
        result = make_process({
            "m/ya.make": module("a.cpp"),
            "m/a.cpp": "",
            "build/conf/sysincl.conf": '@import "${CONF_ROOT}/conf/loop.conf"\n',
            "build/conf/loop.conf": '@import "${CONF_ROOT}/conf/sysincl.conf"\n',
        }, "m")
        self.assertEqual(1, result.returncode)
        self.assertIn("conf: @import loop", result.stderr)

    def test_missing_sysincl_config_is_fatal(self):
        result = make_process({
            "m/ya.make": module("a.cpp"),
            "m/a.cpp": "",
            "build/conf/sysincl.conf": "SYSINCL=build/sysincl/absent.yml\n",
        }, "m")
        self.assertEqual(1, result.returncode)
        self.assertIn("sysincl: build/sysincl/absent.yml: no such file", result.stderr)

    def test_missing_import_is_fatal(self):
        result = make_process({
            "m/ya.make": module("a.cpp"),
            "m/a.cpp": "",
            "build/ymake.core.conf": '@import "${CONF_ROOT}/conf/absent.conf"\n',
        }, "m")
        self.assertEqual(1, result.returncode)
        self.assertIn("conf: @import build/conf/absent.conf: no such file", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
