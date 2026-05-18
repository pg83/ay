package main

import (
	"bufio"
	"crypto/md5"
	"encoding/base32"
	enchex "encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Emitter for PB (Protocol Buffers compile) nodes.
//
// One PB node per .proto in a PROTO_LIBRARY. Invokes cpp_proto_wrapper.py
// driving protoc + cpp_styleguide (and grpc_cpp plugin when grpc is set),
// all from contrib/tools/protoc. descriptor.proto is added to inputs when
// the source imports it; deps and foreign_deps["tool"] carry the tool LD
// refs.

const (
	// Tool module paths for host-walk recursion.
	pbProtocModule        = "contrib/tools/protoc"
	pbCppStyleguideModule = "contrib/tools/protoc/plugins/cpp_styleguide"
	pbGrpcCppModule       = "contrib/tools/protoc/plugins/grpc_cpp"
	pbGrpcPyModule        = "contrib/tools/protoc/plugins/grpc_python"
	pbMypyModule          = "contrib/python/mypy-protobuf/bin/protoc-gen-mypy"

	// pbRuntimeBase is the SOURCE_ROOT-relative prefix for all protobuf
	// runtime headers (under contrib/libs/protobuf/src/). Combined with
	// Source() at use-site to produce the VFS.
	pbRuntimeBase = "contrib/libs/protobuf/src/"

	// abslTstringBase is the SOURCE_ROOT prefix for abseil-cpp-tstring
	// headers, reached transitively from the protobuf runtime via
	// `port_def.inc → y_absl/strings/string_view.h → …`. Consumer
	// PROTO_LIBRARYs do not peer abseil themselves; the scanner cannot
	// resolve y_absl/... without pre-resolved EmitsIncludes.
	abslTstringBase = "contrib/restricted/abseil-cpp-tstring/"
)

// pb tool/asset VFS constants. The `…Path` strings are derived once
// via .String() for cmd_arg slots that take a raw string.
var (
	pbWrapperVFS    = Source("build/scripts/cpp_proto_wrapper.py")
	pbPyWrapperVFS  = Source("build/scripts/gen_py_protos.py")
	pbGrpcCppVFS    = Build("contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	pbDescriptorVFS = Source("contrib/libs/protobuf/src/google/protobuf/descriptor.proto")

	pbWrapperPath     = pbWrapperVFS.String()
	pbPyWrapperPath   = pbPyWrapperVFS.String()
	pbDescriptorProto = pbDescriptorVFS.String()
)

// protobufRuntimeHeaders is the set every protoc-generated .pb.h directly
// #includes. Registered as EmitsIncludes on the .pb.h so the scanner
// closure propagates them into every CC that includes the .pb.h; scanner
// recursion finds their transitive includes. Sorted lex.
var protobufRuntimeHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/arena.h"),
	Source(pbRuntimeBase + "google/protobuf/arenastring.h"),
	Source(pbRuntimeBase + "google/protobuf/extension_set.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_message_reflection.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_message_util.h"),
	Source(pbRuntimeBase + "google/protobuf/io/coded_stream.h"),
	Source(pbRuntimeBase + "google/protobuf/message.h"),
	Source(pbRuntimeBase + "google/protobuf/metadata_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/port_def.inc"),
	Source(pbRuntimeBase + "google/protobuf/port_undef.inc"),
	Source(pbRuntimeBase + "google/protobuf/repeated_field.h"),
	Source(pbRuntimeBase + "google/protobuf/unknown_field_set.h"),
}

// pbDescriptorImporterHeaders are the protobuf runtime headers in CC
// consumers of any .pb.h whose source proto imports
// "google/protobuf/descriptor.proto". Pull in the map/reflection_ops
// cluster protoc emits in reflection metadata for extension-bearing
// protos. Sorted lex.
var pbDescriptorImporterHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/generated_message_bases.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_inl.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/reflection_ops.h"),
}

// pbCcDeepRuntimeHeaders is the deep protobuf+abseil transitive set every
// protoc-generated .pb.cc reaches. Registered as EmitsIncludes on the
// .pb.cc ONLY — never on .pb.h, which has many non-protobuf CC consumers
// that must not inherit the abseil closure. Sorted lex.
var pbCcDeepRuntimeHeaders = []VFS{
	// Group 1: deep protobuf transitive set.
	Source(pbRuntimeBase + "google/protobuf/any.h"),
	Source(pbRuntimeBase + "google/protobuf/arena_align.h"),
	Source(pbRuntimeBase + "google/protobuf/arena_allocation_policy.h"),
	Source(pbRuntimeBase + "google/protobuf/arena_cleanup.h"),
	Source(pbRuntimeBase + "google/protobuf/arena_config.h"),
	Source(pbRuntimeBase + "google/protobuf/arenaz_sampler.h"),
	Source(pbRuntimeBase + "google/protobuf/descriptor.h"),
	Source(pbRuntimeBase + "google/protobuf/endian.h"),
	Source(pbRuntimeBase + "google/protobuf/explicitly_constructed.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_enum_reflection.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_enum_util.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_message_bases.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_message_tctable_decl.h"),
	Source(pbRuntimeBase + "google/protobuf/has_bits.h"),
	Source(pbRuntimeBase + "google/protobuf/implicit_weak_message.h"),
	Source(pbRuntimeBase + "google/protobuf/inlined_string_field.h"),
	Source(pbRuntimeBase + "google/protobuf/io/zero_copy_stream.h"),
	Source(pbRuntimeBase + "google/protobuf/io/zero_copy_stream_impl.h"),
	Source(pbRuntimeBase + "google/protobuf/io/zero_copy_stream_impl_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/json_util.h"),
	Source(pbRuntimeBase + "google/protobuf/map.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_inl.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/map_type_handler.h"),
	Source(pbRuntimeBase + "google/protobuf/message_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/messagext.h"),
	Source(pbRuntimeBase + "google/protobuf/parse_context.h"),
	Source(pbRuntimeBase + "google/protobuf/port.h"),
	Source(pbRuntimeBase + "google/protobuf/reflection_ops.h"),
	Source(pbRuntimeBase + "google/protobuf/repeated_ptr_field.h"),
	Source(pbRuntimeBase + "google/protobuf/serial_arena.h"),
	Source(pbRuntimeBase + "google/protobuf/string_block.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/callback.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/common.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/platform_macros.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/port.h"),
	Source(pbRuntimeBase + "google/protobuf/thread_safe_arena.h"),
	Source(pbRuntimeBase + "google/protobuf/wire_format.h"),
	Source(pbRuntimeBase + "google/protobuf/wire_format_lite.h"),

	// Group 2: abseil-cpp-tstring deep transitive closure reached from
	// port_def.inc → string_view.h → ... (145 entries).
	Source(abslTstringBase + "y_absl/algorithm/algorithm.h"),
	Source(abslTstringBase + "y_absl/algorithm/container.h"),
	Source(abslTstringBase + "y_absl/base/attributes.h"),
	Source(abslTstringBase + "y_absl/base/call_once.h"),
	Source(abslTstringBase + "y_absl/base/casts.h"),
	Source(abslTstringBase + "y_absl/base/config.h"),
	Source(abslTstringBase + "y_absl/base/const_init.h"),
	Source(abslTstringBase + "y_absl/base/dynamic_annotations.h"),
	Source(abslTstringBase + "y_absl/base/internal/atomic_hook.h"),
	Source(abslTstringBase + "y_absl/base/internal/dynamic_annotations.h"),
	Source(abslTstringBase + "y_absl/base/internal/endian.h"),
	Source(abslTstringBase + "y_absl/base/internal/errno_saver.h"),
	Source(abslTstringBase + "y_absl/base/internal/identity.h"),
	Source(abslTstringBase + "y_absl/base/internal/inline_variable.h"),
	Source(abslTstringBase + "y_absl/base/internal/invoke.h"),
	Source(abslTstringBase + "y_absl/base/internal/low_level_alloc.h"),
	Source(abslTstringBase + "y_absl/base/internal/low_level_scheduling.h"),
	Source(abslTstringBase + "y_absl/base/internal/nullability_impl.h"),
	Source(abslTstringBase + "y_absl/base/internal/per_thread_tls.h"),
	Source(abslTstringBase + "y_absl/base/internal/raw_logging.h"),
	Source(abslTstringBase + "y_absl/base/internal/scheduling_mode.h"),
	Source(abslTstringBase + "y_absl/base/internal/spinlock.h"),
	Source(abslTstringBase + "y_absl/base/internal/spinlock_wait.h"),
	Source(abslTstringBase + "y_absl/base/internal/thread_identity.h"),
	Source(abslTstringBase + "y_absl/base/internal/throw_delegate.h"),
	Source(abslTstringBase + "y_absl/base/internal/tsan_mutex_interface.h"),
	Source(abslTstringBase + "y_absl/base/internal/unaligned_access.h"),
	Source(abslTstringBase + "y_absl/base/log_severity.h"),
	Source(abslTstringBase + "y_absl/base/macros.h"),
	Source(abslTstringBase + "y_absl/base/nullability.h"),
	Source(abslTstringBase + "y_absl/base/optimization.h"),
	Source(abslTstringBase + "y_absl/base/options.h"),
	Source(abslTstringBase + "y_absl/base/policy_checks.h"),
	Source(abslTstringBase + "y_absl/base/port.h"),
	Source(abslTstringBase + "y_absl/base/prefetch.h"),
	Source(abslTstringBase + "y_absl/base/thread_annotations.h"),
	Source(abslTstringBase + "y_absl/container/btree_map.h"),
	Source(abslTstringBase + "y_absl/container/fixed_array.h"),
	Source(abslTstringBase + "y_absl/container/flat_hash_map.h"),
	Source(abslTstringBase + "y_absl/container/hash_container_defaults.h"),
	Source(abslTstringBase + "y_absl/container/inlined_vector.h"),
	Source(abslTstringBase + "y_absl/container/internal/btree.h"),
	Source(abslTstringBase + "y_absl/container/internal/btree_container.h"),
	Source(abslTstringBase + "y_absl/container/internal/common.h"),
	Source(abslTstringBase + "y_absl/container/internal/common_policy_traits.h"),
	Source(abslTstringBase + "y_absl/container/internal/compressed_tuple.h"),
	Source(abslTstringBase + "y_absl/container/internal/container_memory.h"),
	Source(abslTstringBase + "y_absl/container/internal/hash_function_defaults.h"),
	Source(abslTstringBase + "y_absl/container/internal/hash_policy_traits.h"),
	Source(abslTstringBase + "y_absl/container/internal/hashtable_debug_hooks.h"),
	Source(abslTstringBase + "y_absl/container/internal/hashtablez_sampler.h"),
	Source(abslTstringBase + "y_absl/container/internal/inlined_vector.h"),
	Source(abslTstringBase + "y_absl/container/internal/layout.h"),
	Source(abslTstringBase + "y_absl/container/internal/raw_hash_map.h"),
	Source(abslTstringBase + "y_absl/container/internal/raw_hash_set.h"),
	Source(abslTstringBase + "y_absl/crc/crc32c.h"),
	Source(abslTstringBase + "y_absl/crc/internal/crc32_x86_arm_combined_simd.h"),
	Source(abslTstringBase + "y_absl/crc/internal/crc32c_inline.h"),
	Source(abslTstringBase + "y_absl/crc/internal/crc_cord_state.h"),
	Source(abslTstringBase + "y_absl/debugging/internal/demangle.h"),
	Source(abslTstringBase + "y_absl/functional/any_invocable.h"),
	Source(abslTstringBase + "y_absl/functional/function_ref.h"),
	Source(abslTstringBase + "y_absl/functional/internal/any_invocable.h"),
	Source(abslTstringBase + "y_absl/functional/internal/function_ref.h"),
	Source(abslTstringBase + "y_absl/hash/hash.h"),
	Source(abslTstringBase + "y_absl/hash/internal/city.h"),
	Source(abslTstringBase + "y_absl/hash/internal/hash.h"),
	Source(abslTstringBase + "y_absl/hash/internal/low_level_hash.h"),
	Source(abslTstringBase + "y_absl/log/absl_check.h"),
	Source(abslTstringBase + "y_absl/log/absl_log.h"),
	Source(abslTstringBase + "y_absl/log/absl_vlog_is_on.h"),
	Source(abslTstringBase + "y_absl/log/internal/check_impl.h"),
	Source(abslTstringBase + "y_absl/log/internal/check_op.h"),
	Source(abslTstringBase + "y_absl/log/internal/conditions.h"),
	Source(abslTstringBase + "y_absl/log/internal/config.h"),
	Source(abslTstringBase + "y_absl/log/internal/log_impl.h"),
	Source(abslTstringBase + "y_absl/log/internal/log_message.h"),
	Source(abslTstringBase + "y_absl/log/internal/nullguard.h"),
	Source(abslTstringBase + "y_absl/log/internal/nullstream.h"),
	Source(abslTstringBase + "y_absl/log/internal/proto.h"),
	Source(abslTstringBase + "y_absl/log/internal/strip.h"),
	Source(abslTstringBase + "y_absl/log/internal/structured_proto.h"),
	Source(abslTstringBase + "y_absl/log/internal/vlog_config.h"),
	Source(abslTstringBase + "y_absl/log/internal/voidify.h"),
	Source(abslTstringBase + "y_absl/log/log_entry.h"),
	Source(abslTstringBase + "y_absl/log/log_sink.h"),
	Source(abslTstringBase + "y_absl/memory/memory.h"),
	Source(abslTstringBase + "y_absl/meta/type_traits.h"),
	Source(abslTstringBase + "y_absl/numeric/bits.h"),
	Source(abslTstringBase + "y_absl/numeric/int128.h"),
	Source(abslTstringBase + "y_absl/numeric/int128_have_intrinsic.inc"),
	Source(abslTstringBase + "y_absl/numeric/int128_no_intrinsic.inc"),
	Source(abslTstringBase + "y_absl/numeric/internal/bits.h"),
	Source(abslTstringBase + "y_absl/profiling/internal/sample_recorder.h"),
	Source(abslTstringBase + "y_absl/strings/cord.h"),
	Source(abslTstringBase + "y_absl/strings/cord_analysis.h"),
	Source(abslTstringBase + "y_absl/strings/cord_buffer.h"),
	Source(abslTstringBase + "y_absl/strings/has_absl_stringify.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_data_edge.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_internal.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_rep_btree.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_rep_btree_navigator.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_rep_btree_reader.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_rep_crc.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cord_rep_flat.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cordz_functions.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cordz_handle.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cordz_info.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cordz_statistics.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cordz_update_scope.h"),
	Source(abslTstringBase + "y_absl/strings/internal/cordz_update_tracker.h"),
	Source(abslTstringBase + "y_absl/strings/internal/resize_uninitialized.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/arg.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/bind.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/checker.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/constexpr_parser.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/extension.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/output.h"),
	Source(abslTstringBase + "y_absl/strings/internal/str_format/parser.h"),
	Source(abslTstringBase + "y_absl/strings/internal/string_constant.h"),
	Source(abslTstringBase + "y_absl/strings/internal/stringify_sink.h"),
	Source(abslTstringBase + "y_absl/strings/numbers.h"),
	Source(abslTstringBase + "y_absl/strings/str_cat.h"),
	Source(abslTstringBase + "y_absl/strings/str_format.h"),
	Source(abslTstringBase + "y_absl/strings/string_view.h"),
	Source(abslTstringBase + "y_absl/synchronization/internal/create_thread_identity.h"),
	Source(abslTstringBase + "y_absl/synchronization/internal/kernel_timeout.h"),
	Source(abslTstringBase + "y_absl/synchronization/internal/per_thread_sem.h"),
	Source(abslTstringBase + "y_absl/synchronization/mutex.h"),
	Source(abslTstringBase + "y_absl/time/civil_time.h"),
	Source(abslTstringBase + "y_absl/time/clock.h"),
	Source(abslTstringBase + "y_absl/time/internal/cctz/include/cctz/civil_time.h"),
	Source(abslTstringBase + "y_absl/time/internal/cctz/include/cctz/civil_time_detail.h"),
	Source(abslTstringBase + "y_absl/time/internal/cctz/include/cctz/time_zone.h"),
	Source(abslTstringBase + "y_absl/time/time.h"),
	Source(abslTstringBase + "y_absl/types/bad_optional_access.h"),
	Source(abslTstringBase + "y_absl/types/bad_variant_access.h"),
	Source(abslTstringBase + "y_absl/types/compare.h"),
	Source(abslTstringBase + "y_absl/types/internal/optional.h"),
	Source(abslTstringBase + "y_absl/types/internal/span.h"),
	Source(abslTstringBase + "y_absl/types/internal/variant.h"),
	Source(abslTstringBase + "y_absl/types/optional.h"),
	Source(abslTstringBase + "y_absl/types/span.h"),
	Source(abslTstringBase + "y_absl/types/variant.h"),
	Source(abslTstringBase + "y_absl/utility/utility.h"),
}

// EmitPB emits a PB node for `protoRelPath` (a SOURCE_ROOT-relative .proto path).
// The *LDRef params are host LD NodeRefs (zeroed when the host walk failed);
// the *Binary params are the corresponding $(B)-rooted tool paths. grpcCppLDRef
// and grpcCppBinary are only used when grpc is true. moduleTag is "cpp_proto"
// for PROTO_LIBRARY modules (nil when absent). sourceRoot is used for
// descriptor-import scanning.
func EmitPB(
	instance ModuleInstance,
	protoRelPath string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	grpcCppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	grpcCppBinary VFS,
	grpc bool,
	moduleTag *string,
	cppOutRoot string,
	duplicateOutputRootInclude bool,
	sourceRoot string,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path
	// Output paths strip the .proto suffix: foo.proto → foo.pb.h / foo.pb.cc.
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")
	grpcPbCC := Build(protoBase + ".grpc.pb.cc")
	grpcPbH := Build(protoBase + ".grpc.pb.h")
	srcVFS := Source(protoRelPath)

	outputs := []VFS{pbH, pbCC}
	if grpc {
		outputs = append(outputs, grpcPbCC, grpcPbH)
	}

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		pbWrapperPath,
		"--outputs",
		pbH.String(),
		pbCC.String(),
	}
	if grpc {
		cmdArgs = append(cmdArgs, grpcPbCC.String(), grpcPbH.String())
	}
	includeRoot := ""
	if cppOutRoot != "" {
		includeRoot = cppOutRoot
	}
	cmdArgs = append(cmdArgs,
		"--",
		protocBinary.String(),
		"-I=./"+includeRoot,
		"-I=$(S)/"+includeRoot,
		"-I=$(B)",
		"-I=$(S)",
	)
	if cppOutRoot != "" {
		cmdArgs = append(cmdArgs, "-I=$(S)/"+cppOutRoot)
		if duplicateOutputRootInclude {
			cmdArgs = append(cmdArgs, "-I=$(S)/"+cppOutRoot)
		}
	} else {
		cmdArgs = append(cmdArgs, protoExtraIncludeArgs(protoRelPath)...)
	}
	cmdArgs = append(cmdArgs, "-I=$(S)/contrib/libs/protobuf/src")
	cmdArgs = append(cmdArgs,
		"-I=$(B)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"--cpp_out=:$(B)/"+cppOutRoot,
		"--cpp_styleguide_out=:$(B)/"+cppOutRoot,
		"--plugin=protoc-gen-cpp_styleguide="+cppStyleguideBinary.String(),
		protoRelPath,
	)
	if grpc {
		cmdArgs = append(cmdArgs,
			"--plugin=protoc-gen-grpc_cpp="+grpcCppBinary.String(),
			"--grpc_cpp_out=$(B)/"+cppOutRoot,
		)
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	protoImports := resolveProtoImports(sourceRoot, protoRelPath)
	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		pbWrapperVFS,
	}
	if grpc {
		inputs = append([]VFS{grpcCppBinary}, inputs...)
	}
	if protoImports != nil && protoImports.HasDescriptor {
		inputs = append(inputs, pbDescriptorVFS)
	}
	inputs = append(inputs, srcVFS)
	if protoImports != nil {
		inputs = append(inputs, protoImports.Imports...)
	}

	// tags come from instance.Platform (["tool"] on host, [] on target);
	// non-nil empty slice keeps JSON `[]`, not `null`.
	tags := instance.Platform.Tags

	targetProps := map[string]string{
		"module_dir": moduleDir,
	}

	if moduleTag != nil {
		targetProps["module_tag"] = *moduleTag
	}

	// deps and foreign_deps both carry the same tool refs.
	var depRefs []NodeRef
	var foreignDepRefs map[string][]NodeRef

	if cppStyleguideLDRef != (NodeRef{}) || protocLDRef != (NodeRef{}) || grpcCppLDRef != (NodeRef{}) {
		var toolRefs []NodeRef
		if cppStyleguideLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}
		if protocLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, protocLDRef)
		}
		if grpcCppLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, grpcCppLDRef)
		}
		depRefs = append([]NodeRef(nil), toolRefs...)
		foreignDepRefs = map[string][]NodeRef{"tool": toolRefs}
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(S)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  inputs,
		Outputs: outputs,
		KV: map[string]string{
			"p":  "PB",
			"pc": "yellow",
		},
		Tags:             tags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		HostPlatform:     instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: foreignDepRefs,
	}

	return emit.Emit(node)
}

func protoExtraIncludeArgs(protoRelPath string) []string {
	if strings.HasPrefix(protoRelPath, "yt/") {
		return []string{"-I=$(S)/yt"}
	}
	return nil
}

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func containsVFS(xs []VFS, want VFS) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func protoCPPModulePath(instance ModuleInstance, d *moduleData) string {
	if d != nil && d.protoNamespace != nil {
		if d.protoNamespaceGlobal {
			return instance.Path
		}
		base := filepath.ToSlash(filepath.Clean(filepath.Dir(*d.protoNamespace)))
		if base != "." && base != "" {
			return base
		}
	}

	return instance.Path
}

func protoCPPOutRoot(d *moduleData) string {
	if d == nil || d.protoNamespace == nil {
		return ""
	}

	root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(*d.protoNamespace)), "/")
	if root == "." {
		return ""
	}

	return root
}

// pbDescriptorImporterExtras returns the witness inputs propagated through
// a protoc-generated .pb.h to its CC consumers: pbDescriptorImporterHeaders
// (7 reflection-cluster headers), cpp_proto_wrapper.py, the proto source,
// and descriptor.proto when imported. Verified by intersecting CC-consumer
// inputs across all .pb.h's in /home/pg/monorepo/yatool_orig/sg2.json.
func pbDescriptorImporterExtras(sourceRoot, protoRelPath string) []includeDirective {
	out := make([]includeDirective, 0, len(pbDescriptorImporterHeaders)+3)
	out = append(out, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
	out = append(out, includeDirective{kind: includeQuoted, target: protoRelPath})
	for _, v := range pbDescriptorImporterHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: v.Rel})
	}

	protoImports := resolveProtoImports(sourceRoot, protoRelPath)
	if protoImports != nil && protoImports.HasDescriptor {
		out = append(out, includeDirective{kind: includeQuoted, target: pbDescriptorVFS.Rel})
	}

	return out
}

type protoImportResolution struct {
	HasDescriptor bool
	Imports       []VFS
}

func resolveProtoImportPath(sourceRoot, importedRel string) string {
	clean := filepath.ToSlash(filepath.Clean(importedRel))
	candidates := []string{clean}
	if !strings.HasPrefix(clean, "yt/") {
		candidates = append(candidates, filepath.ToSlash(filepath.Clean("yt/"+clean)))
	}
	candidates = append(candidates, filepath.ToSlash(filepath.Clean(pbRuntimeBase+clean)))

	for _, cand := range candidates {
		if _, err := os.Stat(filepath.Join(sourceRoot, cand)); err == nil {
			return cand
		}
	}

	return ""
}

// resolveProtoImports returns the transitive raw `.proto` import set for
// `<sourceRoot>/<srcRel>`, preserving the upstream shape: direct imports of
// each file are emitted before their transitive closure, and descriptor.proto
// is surfaced separately because its input slot precedes the source file.
func resolveProtoImports(sourceRoot, srcRel string) *protoImportResolution {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var rootImports []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "import ") {
			continue
		}
		start := strings.IndexByte(line, '"')
		end := strings.LastIndexByte(line, '"')
		if start < 0 || end <= start {
			continue
		}
		rootImports = append(rootImports, line[start+1:end])
	}

	res := &protoImportResolution{}
	seen := map[string]struct{}{}
	scanned := map[string]struct{}{}
	var walk func(string)
	walk = func(rel string) {
		if _, done := scanned[rel]; done {
			return
		}
		scanned[rel] = struct{}{}

		abs := filepath.Join(sourceRoot, rel)
		f, err := os.Open(abs)
		if err != nil {
			return
		}

		var imports []string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "import ") {
				continue
			}
			start := strings.IndexByte(line, '"')
			end := strings.LastIndexByte(line, '"')
			if start < 0 || end <= start {
				continue
			}
			imports = append(imports, line[start+1:end])
		}
		f.Close()

		for _, imp := range imports {
			if imp == "google/protobuf/descriptor.proto" {
				res.HasDescriptor = true
				continue
			}
			resolved := resolveProtoImportPath(sourceRoot, imp)
			if resolved == "" {
				continue
			}
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}
			res.Imports = append(res.Imports, Source(resolved))
		}

		for _, imp := range imports {
			if imp == "google/protobuf/descriptor.proto" {
				continue
			}
			if resolved := resolveProtoImportPath(sourceRoot, imp); resolved != "" {
				walk(resolved)
			}
		}
	}

	for _, imp := range rootImports {
		if imp == "google/protobuf/descriptor.proto" {
			res.HasDescriptor = true
			continue
		}
		resolved := resolveProtoImportPath(sourceRoot, imp)
		if resolved == "" {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		res.Imports = append(res.Imports, Source(resolved))
	}
	for _, imp := range rootImports {
		if imp == "google/protobuf/descriptor.proto" {
			continue
		}
		if resolved := resolveProtoImportPath(sourceRoot, imp); resolved != "" {
			walk(resolved)
		}
	}

	if !res.HasDescriptor && len(res.Imports) == 0 {
		return nil
	}

	return res
}

// protoSrcsResult carries the AR and whole-archive closure emitted for a
// PROTO_LIBRARY's generated .pb.cc/.ev.pb.cc set, surfaced via
// moduleEmitResult. ARRef/ARPath are zero when no .proto/.ev sources.
type protoSrcsResult struct {
	ARRef                NodeRef
	ARPath               *VFS
	GlobalRef            *NodeRef
	GlobalPath           *VFS
	WholeArchiveRefs     []NodeRef
	WholeArchivePaths    []VFS
	WholeArchiveCmdPaths []VFS
}

func protoSourceRelPath(sourceRoot string, instance ModuleInstance, d *moduleData, src string) string {
	moduleRel := filepath.ToSlash(filepath.Clean(instance.Path + "/" + src))
	if sourceRoot != "" {
		if _, err := os.Stat(filepath.Join(sourceRoot, filepath.FromSlash(moduleRel))); err == nil {
			return moduleRel
		}
	}

	baseDir := instance.Path
	if d.srcDir != nil {
		cleaned := filepath.Clean(*d.srcDir)
		if cleaned != "." {
			baseDir = cleaned
		}
	}

	return filepath.ToSlash(filepath.Clean(baseDir + "/" + src))
}

func pyProtoAuxOwnAddIncl(d *moduleData) []VFS {
	out := make([]VFS, 0, 2)
	if d != nil && d.protoNamespace != nil {
		base := filepath.ToSlash(filepath.Clean(*d.protoNamespace))
		if base != "." && base != "" {
			out = append(out, Build(base))
		}
	}
	out = append(out, Source("contrib/libs/python/Include"))
	return out
}

func pyProtoAuxInputClosure(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, aux VFS, seed []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		emits := []includeDirective{
			{kind: includeQuoted, target: "library/cpp/resource/resource.h"},
			{kind: includeQuoted, target: "library/cpp/resource/registry.h"},
		}
		for _, in := range seed {
			if in.IsSource() {
				emits = append(emits, includeDirective{kind: includeQuoted, target: in.Rel})
			}
		}
		registerGeneratedParsedOutput(ctx, instance, "PR", aux, emits)
	}

	scanIn := ModuleCCInputs{
		AddIncl:           pyProtoAuxOwnAddIncl(d),
		PeerAddInclGlobal: pyProtoAuxPeerAddIncl(instance, peerContribs, d),
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	closure := walkClosure(ctx, instance, aux, scanIn)
	if len(closure) == 0 {
		return nil
	}

	return dedupVFS(closure)
}

func pyProtoAuxPeerAddIncl(instance ModuleInstance, peerContribs peerGlobalContribs, d *moduleData) []VFS {
	out := make([]VFS, 0, len(peerContribs.addIncl)+8)
	for _, p := range peerContribs.addIncl {
		if p == Source("contrib/libs/protobuf/src") || p == Source("contrib/restricted/abseil-cpp-tstring") || p == Source("contrib/restricted/abseil-cpp") {
			continue
		}
		out = append(out, p)
	}
	out = append(out,
		Source("contrib/libs/python/Include"),
		Source("contrib/restricted/abseil-cpp"),
		Build("library/python/runtime_py3"),
		Source("contrib/libs/lzma/liblzma/api"),
		Source("contrib/libs/openssl/include"),
		Source("contrib/restricted/libffi/include"),
		Source("contrib/restricted/libffi/configs/"+libffiConfigTriple(instance.Platform)+"/include"),
	)
	if d != nil && d.protoNamespace != nil {
		base := filepath.ToSlash(filepath.Clean(*d.protoNamespace))
		if base != "." && base != "" && base != "contrib/libs/protobuf/src" {
			out = append(out, Build("contrib/libs/protobuf/src"))
		}
	}
	return out
}

func libffiConfigTriple(p *Platform) string {
	switch p.ISA {
	case ISAAArch64:
		return "aarch64-unknown-linux-gnu"
	case ISAX8664:
		return "x86_64-unknown-linux-gnu"
	default:
		return p.Triple
	}
}

func py3ccToolRefs(ctx *genCtx, instance ModuleInstance) (NodeRef, NodeRef, VFS, VFS) {
	py3ccRef, py3ccRaw := ctx.tool("tools/py3cc/bin")
	py3ccBinary := canonicalizePy3ccBinary(py3ccRaw)
	py3ccSlowRef, py3ccSlowBin := ctx.tool("tools/py3cc/slow")
	return py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin
}

func protoPySuffix(modulePath string) string {
	return protoPathID("$S/" + modulePath)[:4]
}

func protoPathID(path string) string {
	sum := md5.Sum([]byte(path))
	encoded := base32.StdEncoding.EncodeToString(sum[:])
	encoded = strings.ToLower(encoded)
	return strings.TrimRight(encoded, "=")
}

func protoResourceHash(items []string, modulePath, moduleTag string) string {
	list := append([]string(nil), items...)
	list = append(list, modulePath)
	sort.Strings(list)

	sum := md5.Sum([]byte(strings.Join(list, ",") + moduleTag))
	return strings.ToLower(enchex.EncodeToString(sum[:]))[:26]
}
