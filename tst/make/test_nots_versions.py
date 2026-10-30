import base64
import json

from tst.make.lib import MakeTest, PROJECT


class NotsVersionsTest(MakeTest):
    def test_node_versions_below_twenty_are_unsupported(self):
        self.package(node="18.20.8")
        resource_path = "devtools/frontend_build_platform/erm/erm-packages.json"
        resources = json.loads((self.root / resource_path).read_text())
        resources["nodejs"]["versions"]["18.20.8"] = resources["nodejs"]["versions"]["22.13.0"]
        self.write_tree(resource_path, json.dumps(resources))
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsupported", result.stderr.lower())

    def test_hermetic_patch_files_are_prepare_inputs(self):
        self.package({"package": "1.2.3"})
        self.write("ya.make", (self.project / "ya.make").read_text().replace("DISABLE_HERMETIC_NODE_MODULES()\n", ""))
        package = json.loads((self.project / "package.json").read_text())
        package["pnpm"] = {"patchedDependencies": {"package@1.2.3": "patches/package.patch"}}
        self.write("package.json", json.dumps(package))
        self.write("patches/package.patch", "diff --git a/answer.js b/answer.js\n")
        prepared = self.node(self.graph("-DAUTOCHECK=yes"), "package.json")
        self.assertIn(f"$(S)/{PROJECT}/patches/package.patch", prepared["inputs"])

    def test_missing_node_resource_does_not_prevent_graph_generation(self):
        self.package()
        self.write_tree("build/platform/nodejs/22.13.0/ya.make", "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\nEND()\n")
        self.node(self.graph(), "output.tar")

    def test_sibling_packages_share_the_registry_settings(self):
        self.package({"package": "1.2.3"})
        files = {name: (self.project / name).read_text() for name in ("ya.make", "package.json", "pnpm-lock.yaml", "answer.js")}
        for directory in ("first", "second"):
            for name, content in files.items():
                if name == "package.json":
                    package = json.loads(content)
                    package["name"] = f"@e2e/{directory}"
                    content = json.dumps(package)
                self.write(f"{directory}/{name}", content)
        self.write("ya.make", "RECURSE(first second)\n")
        graph = self.graph()
        for directory in ("first", "second"):
            self.node(graph, f"{directory}/output.tar")

    def test_package_json_must_be_an_object(self):
        for content in ("null", "[]", "42"):
            with self.subTest(content=content):
                self.package()
                self.write("package.json", content)
                self.assertNotEqual(self.make().returncode, 0)

    def test_malformed_package_json_is_a_configuration_error(self):
        self.package()
        self.write("package.json", '{"name":')
        self.assertNotEqual(self.make().returncode, 0)

    def test_workspace_dependencies_require_relative_paths(self):
        for spec in ("workspace:package", "workspace:*"):
            with self.subTest(spec=spec):
                self.package({"package": "1.2.3"})
                package = json.loads((self.project / "package.json").read_text())
                package["dependencies"]["package"] = spec
                self.write("package.json", json.dumps(package))
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("workspace", result.stderr.lower())

    def test_default_node_resource_version_cannot_be_empty(self):
        self.package()
        package = json.loads((self.project / "package.json").read_text())
        package.pop("engines")
        self.write("package.json", json.dumps(package))
        resource_path = "devtools/frontend_build_platform/erm/erm-packages.json"
        resources = json.loads((self.root / resource_path).read_text())
        resources["nodejs"]["default"] = ""
        self.write_tree(resource_path, json.dumps(resources))
        self.assertNotEqual(self.make().returncode, 0)

    def test_available_node_versions_must_be_stable_versions(self):
        self.package()
        resource_path = "devtools/frontend_build_platform/erm/erm-packages.json"
        resources = json.loads((self.root / resource_path).read_text())
        versions = resources["nodejs"]["versions"]
        versions["e2e-invalid-version"] = next(iter(versions.values()))
        self.write_tree(resource_path, json.dumps(resources))
        self.assertNotEqual(self.make().returncode, 0)

    def lock_resolution(self, resolution):
        self.package({"package": "1.2.3"})
        lock = json.loads((self.project / "pnpm-lock.yaml").read_text())
        lock["packages"]["package@1.2.3"]["resolution"].update(resolution)
        self.write("pnpm-lock.yaml", json.dumps(lock))

    def test_unsupported_lockfile_versions_are_configuration_errors(self):
        for version in ("", "999.0"):
            with self.subTest(version=version):
                self.package({"package": "1.2.3"})
                lock = json.loads((self.project / "pnpm-lock.yaml").read_text())
                lock["lockfileVersion"] = version
                self.write("pnpm-lock.yaml", json.dumps(lock))
                self.assertNotEqual(self.make().returncode, 0)

    def test_tarballs_cannot_use_file_urls(self):
        self.lock_resolution({"tarball": "file:./package-1.2.3.tgz"})
        self.assertNotEqual(self.make().returncode, 0)

    def test_tarballs_cannot_use_an_external_registry(self):
        self.lock_resolution({"tarball": "https://registry.example.com/package/-/package-1.2.3.tgz"})
        self.assertNotEqual(self.make().returncode, 0)

    def test_invalid_tarball_integrities_are_configuration_errors(self):
        for integrity in ("invalid", "unknown-YQ==", "sha512-!notbase64"):
            with self.subTest(integrity=integrity):
                self.lock_resolution({"integrity": integrity})
                self.assertNotEqual(self.make().returncode, 0)

    def test_local_directory_entries_do_not_become_tarball_downloads(self):
        self.package({"package": "1.2.3"})
        lock = json.loads((self.project / "pnpm-lock.yaml").read_text())
        lock["packages"]["unused@file:local"] = {"resolution": {"type": "directory", "directory": "local"}}
        self.write("pnpm-lock.yaml", json.dumps(lock))
        graph = self.graph()
        self.node(graph, "output.tar")
        self.assertNotIn("unused-", json.dumps(graph))

    def test_tarball_urls_without_a_dash_directory_are_preserved(self):
        url = "https://npm.yandex-team.ru/legacy/package-1.2.3.tgz"
        self.lock_resolution({"tarball": url})
        self.assertIn(url, json.dumps(self.graph()))

    def test_relative_and_nested_tarball_urls_are_canonicalized(self):
        canonical = "https://npm.yandex-team.ru/package/-/package-1.2.3.tgz"
        for url in ("package/-/package-1.2.3.tgz", "https://npm.yandex-team.ru/package/-/nested/package-1.2.3.tgz"):
            with self.subTest(url=url):
                self.lock_resolution({"tarball": url})
                self.assertIn(canonical, json.dumps(self.graph()))

    def test_node_engine_whitespace_immediately_after_operators_and_versions(self):
        for constraint in ("=\t22.13.0\t", "=\f22.13.0", "=\r22.13.0\n"):
            with self.subTest(constraint=constraint):
                self.package(node=constraint)
                self.node(self.graph(), "output.tar")

    def test_single_space_before_an_invalid_node_engine_is_an_error(self):
        self.package(node=" x")
        self.assertNotEqual(self.make().returncode, 0)

    def test_scope_punctuation_survives_tarball_url_inference(self):
        names = ("@sc!ope/package", "@sc'ope/package")
        self.package({name: "1.2.3" for name in names})
        serialized = json.dumps(self.graph())
        for name in names:
            self.assertIn(name + "/-/package-1.2.3.tgz", serialized)

    def test_node_engine_whitespace_runs(self):
        whitespace = " \t\f\r\n"
        for constraint in (whitespace + "22.13.0" + whitespace,
                           whitespace + "=" + whitespace + "22.13.0" + whitespace,
                           whitespace + ">=" + whitespace + "22.13.0" + whitespace):
            with self.subTest(constraint=constraint):
                self.package(node=constraint)
                self.node(self.graph(), "output.tar")

    def test_malformed_engine_prefixes_and_unavailable_patch_versions(self):
        for constraint in (" \tbad", " \t", " 2", "2bad", " 22", "22x", "2.3.45", "22.13.100", " =\t\f\r\n"):
            with self.subTest(constraint=constraint):
                self.package(node=constraint)
                self.assertNotEqual(self.make().returncode, 0)

    def test_package_name_initial_character_classes(self):
        names = [prefix + "pkg" for prefix in ("!", "'", "-", "0", "A", "_", "~")]
        names += ["@" + prefix + "Scope/" + prefix + "pkg" for prefix in ("!", "'", "-", "0", "A", "_", "~")]
        self.package({name: "1.2.3" for name in names})
        serialized = json.dumps(self.graph())
        for name in names:
            with self.subTest(package=name):
                self.assertIn(name.rsplit("/", 1)[-1] + "-1.2.3.tgz", serialized)

    def test_zero_patch_prerelease_and_build_character_classes(self):
        versions = ("1.2.0+0Aa-", "1.2.0-0", "1.2.0-0A", "1.2.0-00A", "1.2.0-00-",
                    "1.2.0-00a", "1.2.0-000A", "1.2.0-0-", "1.2.0-A", "1.2.0-a", "1.2.0--")
        packages = {f"package{index}": version for index, version in enumerate(versions)}
        self.package(packages)
        serialized = json.dumps(self.graph())
        for name, version in packages.items():
            self.assertIn(name + "-" + version + ".tgz", serialized)

    def test_invalid_multi_digit_major_and_numeric_prerelease(self):
        for version in ("123", "123x.2.3", "1.2.3-000"):
            with self.subTest(version=version):
                self.package({"package": version})
                self.assertNotEqual(self.make().returncode, 0)

    def test_empty_and_embedded_nul_dependency_versions_are_rejected(self):
        for version in ("", "\0", "1.2.3\0", "1.2.3-alpha\0tail", "1.2.3+build\0"):
            with self.subTest(version=version):
                self.package({"package": version})
                self.assertNotEqual(self.make().returncode, 0)

    def test_empty_and_incomplete_scoped_dependency_names_are_rejected(self):
        for name in ("", "@", "@scope", "@scope//package", "package\0suffix", "@scope/pack\0age"):
            with self.subTest(name=name):
                self.package({name: "1.2.3"})
                self.assertNotEqual(self.make().returncode, 0)

    def package(self, packages=None, node="22.13.0"):
        packages = packages or {}
        self.write("ya.make", "TS_PACKAGE()\nWITH_NODE_MODULES()\nDISABLE_HERMETIC_NODE_MODULES()\nEND()\n")
        self.write("package.json", json.dumps({
            "name": "@e2e/versions", "version": "1.0.0", "engines": {"node": node},
            "dependencies": packages,
        }))
        self.write("pnpm-lock.yaml", json.dumps({
            "lockfileVersion": "9.0",
            "importers": {".": {"dependencies": {
                name: {"specifier": version, "version": version} for name, version in packages.items()
            }}},
            "packages": {name + "@" + version: {"resolution": {
                "integrity": "sha512-" + base64.b64encode(bytes(64)).decode(),
            }} for name, version in packages.items()},
            "snapshots": {name + "@" + version: {} for name, version in packages.items()},
        }))
        self.write("answer.js", "module.exports = 42;\n")

    def test_node_engine_exact_and_lower_bound(self):
        for constraint in ("22.13.0", "=22.13.0", " = 22.13.0 ", ">=22.13.0", "\t>=\t22.13.0\r\n"):
            with self.subTest(constraint=constraint):
                self.package(node=constraint)
                self.node(self.graph(), "output.tar")

    def test_invalid_node_engine_ranges_are_configuration_errors(self):
        for constraint in ("banana", "22.13", "22..0", "22.x.0", ">=", "= ", "22.13.0 extra", "9999.0.0"):
            with self.subTest(constraint=constraint):
                self.package(node=constraint)
                self.assertNotEqual(self.make().returncode, 0)

    def test_prerelease_build_and_scoped_dependency_keys(self):
        versions = ("0.0.0", "12.34.56", "1.2.3-0", "1.2.3-1.2", "1.2.3-01alpha.2beta-3",
                    "1.2.3-alpha.beta-1", "1.2.3+build.001", "1.2.3-0.alpha+build.001-2")
        packages = {(f"@Scope.pkg/package{index}" if index % 2 else f"legacy_~!'*-pkg{index}"): version
                    for index, version in enumerate(versions)}
        self.package(packages)
        graph = self.graph()
        self.node(graph, "output.tar")
        serialized = json.dumps(graph)
        for name, version in packages.items():
            with self.subTest(package=name):
                self.assertIn(name.rsplit("/", 1)[-1] + "-" + version + ".tgz", serialized)

    def test_invalid_dependency_versions_are_configuration_errors(self):
        for version in ("1", "1.2", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-", "1.2.3+", "1.2.3-alpha..1", "1.2.3+build..1"):
            with self.subTest(version=version):
                self.package({"package": version})
                self.assertNotEqual(self.make().returncode, 0)

    def test_invalid_dependency_names_are_configuration_errors(self):
        for name in (".package", "@scope/", "@scope/package/nested", "package space", "package?query", "package#fragment"):
            with self.subTest(name=name):
                self.package({name: "1.2.3"})
                self.assertNotEqual(self.make().returncode, 0)

    def test_truncated_and_non_semver_dependency_versions(self):
        versions = ("0.", "0.0.", "12.", "12.34.", "12.34.x", "12.x.56", "x.34.56",
                    "1.2.3-alpha.", "1.2.3-1.", "1.2.3-1.02", "1.2.3-1.2.",
                    "1.2.3-alpha+", "1.2.3+build.", "1.2.3+build.1.",
                    "1.2.3+build?", "1.2.3-alpha?", "1.2.3.4", "1.2.3 extra")
        for version in versions:
            with self.subTest(version=version):
                self.package({"package": version})
                self.assertNotEqual(self.make().returncode, 0)

    def test_numeric_prerelease_prefixes_and_hyphen_identifiers(self):
        versions = ("1.2.3--", "1.2.3-123alpha", "1.2.3-alpha.0.20", "1.2.3-00alpha.-.0+build.12.-")
        packages = {f"package{index}": version for index, version in enumerate(versions)}
        self.package(packages)
        graph = self.graph()
        serialized = json.dumps(graph)
        for name, version in packages.items():
            self.assertIn(name + "-" + version + ".tgz", serialized)

    def test_truncated_and_unsupported_node_engine_operators(self):
        for constraint in (">", " >", ">= ", "=22.", "=22.13.", "22.", "22.13.", "22.13.x",
                           "22.x.0", ">22.13.0", "<=22.13.0", "==22.13.0"):
            with self.subTest(constraint=constraint):
                self.package(node=constraint)
                self.assertNotEqual(self.make().returncode, 0)
