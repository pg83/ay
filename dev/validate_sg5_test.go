package main

import "testing"

func TestCompareRuntimePy3Node_IgnoresHostStatsUIDEquality(t *testing.T) {
	ref := sg5Node{
		Cmds: []sg5Cmd{{
			CmdArgs: []string{
				"$(B)/library/python/runtime_py3/stage0pycc/stage0pycc",
				"mod=library/python/runtime_py3/__res.py",
				"$(S)/library/python/runtime_py3/__res.py",
				"$(B)/library/python/runtime_py3/__res.pyc",
				"mod=library/python/runtime_py3/sitecustomize.py",
				"$(S)/library/python/runtime_py3/sitecustomize.py",
				"$(B)/library/python/runtime_py3/sitecustomize.pyc",
			},
			Env: map[string]string{
				"ARCADIA_ROOT_DISTBUILD": "$(S)",
				"PYTHONHASHSEED":         "0",
			},
		}},
		Env: map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
			"PYTHONHASHSEED":         "0",
		},
		HostPlatform: true,
		Inputs: []string{
			"$(B)/library/python/runtime_py3/stage0pycc/stage0pycc",
			"$(S)/library/python/runtime_py3/__res.py",
			"$(S)/library/python/runtime_py3/sitecustomize.py",
		},
		KV: map[string]string{
			"p":        "PR",
			"pc":       "yellow",
			"show_out": "yes",
		},
		Outputs: []string{
			"$(B)/library/python/runtime_py3/__res.pyc",
			"$(B)/library/python/runtime_py3/sitecustomize.pyc",
		},
		Platform:   "default-linux-x86_64",
		Sandboxing: true,
		StatsUID:   "0123456789abcdef0123456789abcdef",
		TargetProperties: map[string]string{
			"module_dir": "library/python/runtime_py3",
		},
	}
	our := ref
	our.StatsUID = "fedcba9876543210fedcba9876543210"

	if err := compareRuntimePy3Node(our, ref); err != nil {
		t.Fatalf("compareRuntimePy3Node() error = %v", err)
	}
}

func TestCompareGrpcUpbDescriptorNode_IgnoresHeaderInputsButRejectsCmdDrift(t *testing.T) {
	ref := sg5Node{
		Cmds: []sg5Cmd{{
			CmdArgs: []string{
				"$(CLANG-1274503668)/bin/clang",
				"-o",
				"$(B)/contrib/libs/grpc/third_party/upb/__/__/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c.pic.o",
				"$(S)/contrib/libs/grpc/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c",
			},
			Env: map[string]string{
				"ARCADIA_ROOT_DISTBUILD": "$(S)",
			},
		}},
		Env: map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
		},
		HostPlatform: true,
		Inputs: []string{
			"$(S)/contrib/libs/grpc/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c",
			"$(S)/contrib/libs/grpc/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.h",
		},
		KV: map[string]string{
			"p":  "CC",
			"pc": "green",
		},
		Outputs: []string{
			"$(B)/contrib/libs/grpc/third_party/upb/__/__/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c.pic.o",
		},
		Platform:   "default-linux-x86_64",
		Sandboxing: true,
		StatsUID:   "11111111111111111111111111111111",
		TargetProperties: map[string]string{
			"module_dir": "contrib/libs/grpc/third_party/upb",
		},
	}
	our := ref
	our.StatsUID = "22222222222222222222222222222222"
	our.Inputs = []string{
		"$(S)/contrib/libs/grpc/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c",
		"$(S)/contrib/libs/grpc/src/core/ext/upb-gen/google/protobuf/descriptor_other.h",
	}

	if err := compareGrpcUpbDescriptorNode(our, ref); err != nil {
		t.Fatalf("compareGrpcUpbDescriptorNode() with header drift error = %v", err)
	}

	our.Cmds = []sg5Cmd{{
		CmdArgs: []string{
			"$(CLANG-1274503668)/bin/clang",
			"-o",
			"$(B)/contrib/libs/grpc/third_party/upb/__/__/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c.pic.o",
			"$(S)/contrib/libs/grpc/src/core/ext/upb-gen/google/protobuf/descriptor_other.c",
		},
		Env: map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
		},
	}}

	if err := compareGrpcUpbDescriptorNode(our, ref); err == nil {
		t.Fatal("compareGrpcUpbDescriptorNode() accepted source-path drift")
	}
}

func TestCompareYdbdRootTokens_RejectsExtraLinkToken(t *testing.T) {
	ref := sg5Node{
		Cmds: []sg5Cmd{
			{
				CmdArgs: []string{
					"$(YMAKE_PYTHON3-1002064631)/bin/python3",
					"$(S)/build/scripts/vcs_info.py",
					"$(VCS)/vcs.json",
				},
				Env: map[string]string{
					"ARCADIA_ROOT_DISTBUILD": "$(S)",
				},
			},
			{
				CmdArgs: []string{
					"$(CLANG-1274503668)/bin/clang",
					"-o",
					"$(B)/ydb/apps/ydbd/__vcs_version__.c.o",
					"$(B)/ydb/apps/ydbd/__vcs_version__.c",
				},
				Env: map[string]string{
					"DYLD_LIBRARY_PATH": "$(CLANG-1274503668)/lib:$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
				},
			},
			{
				CmdArgs: []string{
					"$(YMAKE_PYTHON3-1002064631)/bin/python3",
					"$(S)/build/scripts/link_exe.py",
					"$(CLANG-1274503668)/bin/llvm-objcopy",
					"$(CLANG-1274503668)/bin/clang++",
					"--ld-path=$(LLD_ROOT-3107549726)/bin/ld.lld",
					"-o",
					"$(B)/ydb/apps/ydbd/ydbd",
				},
				Env: map[string]string{
					"DYLD_LIBRARY_PATH": "$(CLANG-1274503668)/lib:$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
				},
			},
		},
	}
	our := ref
	our.Cmds = append([]sg5Cmd(nil), ref.Cmds...)
	our.Cmds[2] = sg5Cmd{
		CmdArgs: []string{
			"$(YMAKE_PYTHON3-1002064631)/bin/python3",
			"$(S)/build/scripts/link_exe.py",
			"$(CLANG-1274503668)/bin/llvm-objcopy",
			"$(CLANG-1274503668)/bin/clang++",
			"--ld-path=$(LLD_ROOT-3107549726)/bin/ld.lld",
			"$(LLD_ROOT)/bin/ld.lld",
			"-o",
			"$(B)/ydb/apps/ydbd/ydbd",
		},
		Env: map[string]string{
			"DYLD_LIBRARY_PATH": "$(CLANG-1274503668)/lib:$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
		},
	}

	if err := compareYdbdRootTokens(our, ref); err == nil {
		t.Fatal("compareYdbdRootTokens() accepted extra unhashed link token")
	}
}
