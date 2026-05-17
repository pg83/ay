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

// pb.go — emitter for PB (Protocol Buffers compile) nodes.
//
// One PB node per .proto in a PROTO_LIBRARY. Invokes cpp_proto_wrapper.py
// driving protoc + cpp_styleguide plugin (both from contrib/tools/protoc).
//
// inputs = [cpp_styleguide, protoc, cpp_proto_wrapper.py, $(S)/<src>,
//           descriptor.proto?] — descriptor.proto when the source imports it.
// deps and foreign_deps["tool"] both carry [cpp_styleguide_LD, protoc_LD].
// target_properties: module_dir + module_tag:"cpp_proto".

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

// pb tool/asset VFS constants. The `…Path` legacy strings are
// derived once via .String() and used wherever a cmd_arg expects a
// raw string.
var (
	pbWrapperVFS       = Source("build/scripts/cpp_proto_wrapper.py")
	pbPyWrapperVFS     = Source("build/scripts/gen_py_protos.py")
	pbProtocBinaryVFS  = Build("contrib/tools/protoc/protoc")
	pbCppStyleguideVFS = Build("contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide")
	pbGrpcCppVFS       = Build("contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	pbGrpcPyVFS        = Build("contrib/tools/protoc/plugins/grpc_python/grpc_python")
	pbMypyVFS          = Build("contrib/python/mypy-protobuf/bin/protoc-gen-mypy/protoc-gen-mypy")
	pbDescriptorVFS    = Source("contrib/libs/protobuf/src/google/protobuf/descriptor.proto")

	pbWrapperPath       = pbWrapperVFS.String()
	pbPyWrapperPath     = pbPyWrapperVFS.String()
	pbProtocBinaryPath  = pbProtocBinaryVFS.String()
	pbCppStyleguidePath = pbCppStyleguideVFS.String()
	pbGrpcCppPath       = pbGrpcCppVFS.String()
	pbGrpcPyPath        = pbGrpcPyVFS.String()
	pbMypyPath          = pbMypyVFS.String()
	pbDescriptorProto   = pbDescriptorVFS.String()
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
// .pb.cc output ONLY — NOT on the .pb.h: .pb.h is consumed by ~100
// non-protobuf CC nodes that must not inherit the abseil closure;
// .pb.cc has exactly one CC consumer per file, so the closure is
// tightly scoped.
//
// Group 1: deep protobuf set via port_def.inc + message/runtime chain
// (descriptor.h, parse_context.h, map.h, wire_format*.h, stubs/*, …).
// Group 2: abseil-cpp-tstring transitive closure via port_def.inc →
// y_absl/strings/string_view.h → … . libcxx <vector>/<string>/… inside
// y_absl resolve through the consumer's own libcxx peer. Sorted lex.
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
// `cppStyleguideLDRef` and `protocLDRef` are the host LD NodeRefs for the two
// tool programs (zeroed when the host walk failed). `cppStyleguideBinary` and
// `protocBinary` are the $(B)-rooted paths for the tool binaries.
// `moduleTag` is "cpp_proto" for PROTO_LIBRARY modules (nil when absent).
// `sourceRoot` is the absolute path to the source tree root (for descriptor-import scanning).
//
// Returns the emitted NodeRef.
func EmitPB(
	instance ModuleInstance,
	protoRelPath string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	grpcCppLDRef NodeRef,
	cppStyleguideBinary string,
	protocBinary string,
	grpcCppBinary string,
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
		protocBinary,
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
		"--plugin=protoc-gen-cpp_styleguide="+cppStyleguideBinary,
		protoRelPath,
	)
	if grpc {
		cmdArgs = append(cmdArgs,
			"--plugin=protoc-gen-grpc_cpp="+grpcCppBinary,
			"--grpc_cpp_out=$(B)/"+cppOutRoot,
		)
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// inputs: [cpp_styleguide, protoc, wrapper, source, optionally descriptor.proto]
	protoImports := resolveProtoImports(sourceRoot, protoRelPath)
	inputs := []VFS{
		pbCppStyleguideVFS,
		pbProtocBinaryVFS,
		pbWrapperVFS,
	}
	if grpc {
		inputs = append([]VFS{pbGrpcCppVFS}, inputs...)
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

	// deps and foreign_deps both carry the two tool refs.
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
func pbDescriptorImporterExtras(sourceRoot, protoRelPath string) []VFS {
	out := make([]VFS, 0, len(pbDescriptorImporterHeaders)+3)
	out = append(out, pbWrapperVFS)
	out = append(out, Source(protoRelPath))
	out = append(out, pbDescriptorImporterHeaders...)

	protoImports := resolveProtoImports(sourceRoot, protoRelPath)
	if protoImports != nil && protoImports.HasDescriptor {
		out = append(out, pbDescriptorVFS)
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

// emitProtoSrcs emits PB/EV nodes for .proto/.ev entries in d.srcs when
// the module is a PROTO_LIBRARY. Walks host protoc + cpp_styleguide once
// (cached in genCtx memo).
//
// For module name == PROTO_LIBRARY, also emits the downstream CC per
// generated .pb.cc/.ev.pb.cc and an AR archiving them into
// `lib<dotted-path>.a` with module_tag=cpp_proto. peerContribs carries
// the transitive per-axis peer-GLOBAL union the header-only walker
// computed so flags reach consumer CCs.
//
// Returns (ARRef, ARPath) when a PROTO_LIBRARY AR was emitted so the
// caller can surface it through moduleEmitResult's archive closure;
// nil when no .proto/.ev sources.
type protoSrcsResult struct {
	ARRef                NodeRef
	ARPath               *VFS
	GlobalRef            *NodeRef
	GlobalPath           *VFS
	WholeArchiveRefs     []NodeRef
	WholeArchivePaths    []string
	WholeArchiveCmdPaths []string
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

func protoPythonResourceKey(instance ModuleInstance, d *moduleData, src, suffix string) string {
	base := strings.TrimSuffix(src, ".proto")

	if d.pyNamespace == nil {
		return instance.Path + "/" + base + suffix
	}
	if *d.pyNamespace == "." {
		return base + suffix
	}

	nsPath := strings.ReplaceAll(*d.pyNamespace, ".", "/")
	return filepath.ToSlash(filepath.Clean(nsPath + "/" + filepath.Base(base) + suffix))
}

func moduleExcludesTag(d *moduleData, tag string) bool {
	return d != nil && d.excludeTags != nil && d.excludeTags[tag]
}

func protoPythonNamespaceArg(d *moduleData) string {
	if d == nil || d.protoNamespace == nil {
		return "/"
	}

	return "/" + filepath.ToSlash(filepath.Clean(*d.protoNamespace))
}

func emitProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs) *protoSrcsResult {
	var protoSrcs, evSrcs []string

	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src, ".proto"):
			protoSrcs = append(protoSrcs, src)
		case strings.HasSuffix(src, ".ev"):
			evSrcs = append(evSrcs, src)
		}
	}

	if len(protoSrcs) == 0 && len(evSrcs) == 0 {
		return nil
	}

	switch instance.Language {
	case LangPy:
		return emitPyProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs)
	default:
		return emitCPPProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs)
	}
}

func emitCPPProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, protoSrcs, evSrcs []string) *protoSrcsResult {
	// Walk host protoc and cpp_styleguide tool programs.
	cppStyleguideBinary := pbCppStyleguidePath
	protocBinary := pbProtocBinaryPath
	grpcCppBinary := pbGrpcCppPath

	var cppStyleguideLDRef, protocLDRef, grpcCppLDRef NodeRef

	protocHostInst := NewToolInstance(ctx.host, pbProtocModule)
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		if result.LDPath != nil {
			protocBinary = *result.LDPath
		}
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	cppStyleguideHostInst := NewToolInstance(ctx.host, pbCppStyleguideModule)
	cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

	if exc := Try(func() {
		result := genModule(ctx, cppStyleguideHostInst)
		cppStyleguideLDRef = result.LDRef
		if result.LDPath != nil {
			cppStyleguideBinary = *result.LDPath
		}
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	if d.grpc {
		grpcCppHostInst := NewToolInstance(ctx.host, pbGrpcCppModule)
		grpcCppHostInst.Flags = inferFlagsFromPath(pbGrpcCppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, grpcCppHostInst)
			grpcCppLDRef = result.LDRef
			if result.LDPath != nil {
				grpcCppBinary = *result.LDPath
			}
		}); exc != nil {
			_ = exc
		}
	}

	// Collect per-codegen-source (genRef, .pb.cc path) pairs so the AR
	// step can fold them into ccRefs/ccOutputs/memberInputs in
	// declaration order.
	type protoCodegenOutput struct {
		genRef  NodeRef // PB or EV node ref (used as Generator dep for the downstream CC)
		pbCC    VFS     // generated .pb.cc / .ev.pb.cc BUILD_ROOT path
		srcRel  string  // module-relative source-with-codegen-suffix (".pb.cc" appended)
		primSrc VFS     // primary source path ($(S)/<module>/<src>) for AR memberInputs
	}

	var codegenOutputs []protoCodegenOutput
	duplicateOutputRootInclude := false
	if cppOutRoot := protoCPPOutRoot(d); cppOutRoot != "" {
		duplicateOutputRootInclude = slicesContains(peerContribs.addIncl, "$(B)/"+cppOutRoot)
	}

	// Emit PB nodes.
	for _, src := range protoSrcs {
		protoRelPath := protoSourceRelPath(ctx.sourceRoot, instance, d, src)

		pbRef := EmitPB(
			instance, protoRelPath, cppStyleguideLDRef, protocLDRef,
			grpcCppLDRef, cppStyleguideBinary, protocBinary,
			grpcCppBinary, d.grpc,
			stringPtr("cpp_proto"), protoCPPOutRoot(d), duplicateOutputRootInclude, ctx.sourceRoot, ctx.emit)

		// Register the .pb.h with EmitsIncludes: .pb.h's of every imported
		// proto plus the constant protobuf runtime header set.
		protoBase := strings.TrimSuffix(protoRelPath, ".proto")
		pbH := Build(protoBase + ".pb.h")
		pbCC := Build(protoBase + ".pb.cc")
		grpcPbH := Build(protoBase + ".grpc.pb.h")
		grpcPbCC := Build(protoBase + ".grpc.pb.cc")

		// Stash the PB NodeRef under both output paths on the emitting
		// platform so resolveCodegenDepRefs can thread it as a direct dep
		// on consumer CCs whose IncludeInputs carry the .pb.h/.pb.cc path.
		// Keyed per-platform: x86_64 consumers reach the x86_64 PB,
		// aarch64 consumers reach the aarch64 PB.
		pbKey := codegenOutputKey{platform: instance.Platform}
		pbKey.path = pbH
		ctx.pbOutputs[pbKey] = pbRef
		pbKey.path = pbCC
		ctx.pbOutputs[pbKey] = pbRef
		if d.grpc {
			pbKey.path = grpcPbH
			ctx.pbOutputs[pbKey] = pbRef
			pbKey.path = grpcPbCC
			ctx.pbOutputs[pbKey] = pbRef
		}
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			directImports := protoDirectImportIncludes(ctx.sourceRoot, protoRelPath, protoCPPOutRoot(d))
			extras := pbDescriptorImporterExtras(ctx.sourceRoot, protoRelPath)
			emitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(extras))
			emitsIncludes = append(emitsIncludes, directImports...)
			emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
			emitsIncludes = append(emitsIncludes, extras...)
			registerGeneratedOutput(ctx, instance, "PB", pbH, emitsIncludes)
			// Register the .pb.cc output: protoc emits `#include
			// "<base>.pb.h"` plus the protobuf runtime headers; the .pb.cc.o
			// consumer also reaches the deep protobuf+abseil-cpp-tstring
			// transitive closure (pbCcDeepRuntimeHeaders), plus the .proto
			// source itself and cpp_proto_wrapper.py. Scope is narrow: ONLY
			// on the .pb.cc, never the .pb.h — broad .pb.h consumers must
			// NOT inherit the abseil closure.
			pbCCEmits := make([]VFS, 0, 3+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders))
			pbCCEmits = append(pbCCEmits, pbH)
			pbCCEmits = append(pbCCEmits, Source(protoRelPath))
			pbCCEmits = append(pbCCEmits, pbWrapperVFS)
			pbCCEmits = append(pbCCEmits, protobufRuntimeHeaders...)
			pbCCEmits = append(pbCCEmits, pbCcDeepRuntimeHeaders...)
			registerGeneratedOutput(ctx, instance, "PB", pbCC, pbCCEmits)
			if d.grpc {
				grpcCCEmits := make([]VFS, 0, len(pbCCEmits))
				grpcCCEmits = append(grpcCCEmits, grpcPbH)
				grpcCCEmits = append(grpcCCEmits, Source(protoRelPath))
				grpcCCEmits = append(grpcCCEmits, pbWrapperVFS)
				grpcCCEmits = append(grpcCCEmits, protobufRuntimeHeaders...)
				grpcCCEmits = append(grpcCCEmits, pbCcDeepRuntimeHeaders...)
				registerGeneratedOutput(ctx, instance, "PB", grpcPbCC, grpcCCEmits)
			}
		}

		// Stash the (PB ref, .pb.cc, src-with-suffix) for downstream-CC + AR.
		cppInstance := instance
		cppInstance.Path = protoCPPModulePath(instance, d)
		ccSrcRel := strings.TrimPrefix(protoBase+".pb.cc", cppInstance.Path+"/")
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  pbRef,
			pbCC:    pbCC,
			srcRel:  ccSrcRel,
			primSrc: Source(protoRelPath),
		})
		if d.grpc {
			grpcSrcRel := strings.TrimPrefix(protoBase+".grpc.pb.cc", cppInstance.Path+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  pbRef,
				pbCC:    grpcPbCC,
				srcRel:  grpcSrcRel,
				primSrc: Source(protoRelPath),
			})
		}
	}

	// Emit EV nodes (PROTO_LIBRARY with .ev sources → module_tag:"cpp_proto").
	if len(evSrcs) > 0 {
		event2cppBinary := evEvent2cppBinaryPath
		var event2cppLDRef NodeRef

		event2cppHostInst := NewToolInstance(ctx.host, evEvent2cppModule)
		event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, event2cppHostInst)
			event2cppLDRef = result.LDRef
			if result.LDPath != nil {
				event2cppBinary = *result.LDPath
			}
		}); exc != nil {
			_ = exc
		}

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.sourceRoot, instance, d, src)

			evRef := EmitEV(
				instance, evRelPath, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				stringPtr("cpp_proto"), ctx.sourceRoot, ctx.emit)

			// Register .ev.pb.h with EmitsIncludes: .ev source's direct
			// imports + protobuf runtime headers + EV-specific runtime
			// headers (util/* + eventlog).
			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")

			// Stash the EV NodeRef under both outputs on the emitting
			// platform. See PB branch above for keying rationale.
			evKey := codegenOutputKey{platform: instance.Platform}
			evKey.path = evH
			ctx.evOutputs[evKey] = evRef
			evKey.path = evPbCC
			ctx.evOutputs[evKey] = evRef
			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath, protoCPPOutRoot(d))
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				emitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders)+len(evExtras))
				emitsIncludes = append(emitsIncludes, directImports...)
				emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, eventRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, evExtras...)
				registerGeneratedOutput(ctx, instance, "EV", evH, emitsIncludes)
				// Register .ev.pb.cc: event2cpp emits `#include
				// "<base>.ev.pb.h"` plus protobuf + event runtime headers.
				ccEmits := make([]VFS, 0, 1+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders))
				ccEmits = append(ccEmits, evH)
				ccEmits = append(ccEmits, protobufRuntimeHeaders...)
				ccEmits = append(ccEmits, eventRuntimeHeaders...)
				registerGeneratedOutput(ctx, instance, "EV", evPbCC, ccEmits)
			}

			cppInstance := instance
			cppInstance.Path = protoCPPModulePath(instance, d)
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  evRef,
				pbCC:    evPbCC,
				srcRel:  evSrcRel,
				primSrc: Source(evRelPath),
			})
		}
	}

	// For true PROTO_LIBRARY modules, emit the downstream CC per generated
	// .pb.cc/.ev.pb.cc and the AR archiving them. LIBRARY callers handle
	// their own downstream-CC + AR aggregation in emitOneSource.
	if d.moduleStmt.Name != "PROTO_LIBRARY" || len(codegenOutputs) == 0 {
		return nil
	}

	// Compose ModuleCCInputs for the downstream CCs. Per-axis peer-GLOBAL
	// slices come from the header-only walker's peerContribs.
	// NoStdInc modules zero their own GLOBAL CFLAGS.
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if instance.Flags.NoStdInc {
		ownCFlagsGlobalSelf = nil
		ownCXXFlagsGlobalSelf = nil
		ownCOnlyFlagsGlobalSelf = nil
	}

	dedupedAddIncl := mergeDedup(d.addIncl, nil)

	moduleInputs := ModuleCCInputs{
		AddIncl:              dedupedAddIncl,
		PeerAddInclGlobal:    peerContribs.addIncl,
		CFlags:               d.cFlags,
		CXXFlags:             d.cxxFlags,
		COnlyFlags:           d.cOnlyFlags,
		OwnCFlagsGlobal:      ownCFlagsGlobalSelf,
		OwnCXXFlagsGlobal:    ownCXXFlagsGlobalSelf,
		OwnCOnlyFlagsGlobal:  ownCOnlyFlagsGlobalSelf,
		PeerCFlagsGlobal:     peerContribs.cFlags,
		PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
		PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
		AutoPeerCFlags:       defaultPeerCFlags(ctx, instance, d),
		SrcDir:               d.srcDir,
		SourceRoot:           ctx.sourceRoot,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		ModuleTag:            stringPtr("cpp_proto"),
	}

	// Per-source downstream-CC emission for the PROTO_LIBRARY context.
	cppInstance := instance
	cppInstance.Path = protoCPPModulePath(instance, d)
	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]VFS, 0, len(codegenOutputs))
	memberInputs := make([]VFS, 0, 64)
	memberInputsSeen := make(map[VFS]struct{})

	addMemberInputs := func(paths []VFS) {
		for _, p := range paths {
			if _, dup := memberInputsSeen[p]; dup {
				continue
			}
			memberInputsSeen[p] = struct{}{}
			memberInputs = append(memberInputs, p)
		}
	}

	wireFormatVFS := Source(pbRuntimeBase + "google/protobuf/wire_format.h")
	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IsGenerated = true
		ccIn.Generator = co.genRef
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = walkClosure(ctx, instance, co.pbCC, moduleInputs)
		// .ev.pb.cc.o consumer must not carry its own .ev.pb.h in inputs[]
		// (REF omits the self-include). Drop just the sibling header.
		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			selfH := Build(strings.TrimSuffix(co.pbCC.Rel, ".cc") + ".h")
			filtered := make([]VFS, 0, len(ccIn.IncludeInputs))
			for _, in := range ccIn.IncludeInputs {
				if in == selfH {
					continue
				}
				filtered = append(filtered, in)
			}
			ccIn.IncludeInputs = filtered
		}
		// .ev.pb.cc gets wire_format.h post-closure (registry-side leaks through
		// .ev.pb.h to over-emit; .pb.cc gets it via pbCcDeepRuntimeHeaders).
		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
		}
		// Cross-codegen deps via .pb.h imports.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, co.genRef)

		ccRef, ccOut := EmitCC(cppInstance, co.srcRel, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)

		// AR memberInputs: primary source first, then the CC's include
		// closure.
		perCC := make([]VFS, 0, 1+len(ccIn.IncludeInputs))
		perCC = append(perCC, co.primSrc)
		perCC = append(perCC, ccIn.IncludeInputs...)
		addMemberInputs(perCC)
	}

	// AR emission with module_tag=cpp_proto.
	arBaseName := ArchiveName(instance.Path)
	archivePath := Build(instance.Path + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, stringPtr("cpp_proto"), ccRefs, ccOutputs, nil, memberInputs, nil, ctx.host, ctx.emit)

	return &protoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}

func emitPyProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, protoSrcs, evSrcs []string) *protoSrcsResult {
	if len(evSrcs) > 0 {
		ThrowFmt("gen: py-addressed PROTO_LIBRARY %s with .ev sources is not modelled", instance.Path)
	}
	if len(protoSrcs) == 0 {
		return nil
	}

	protocBinary := pbProtocBinaryPath
	var protocLDRef NodeRef

	protocHostInst := NewToolInstance(ctx.host, pbProtocModule)
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		if result.LDPath != nil {
			protocBinary = *result.LDPath
		}
	}); exc != nil {
		_ = exc
	}

	var cppSibling *moduleEmitResult
	if !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance
		cppInstance.Language = LangCPP
		cppSibling = genModule(ctx, cppInstance)
	}

	var pyProtoRefs []NodeRef
	var pyProtoOutputs []VFS
	var pyProtoMemberInputs []VFS
	var auxEntries []pyProtoAuxEntry

	for _, src := range protoSrcs {
		auxEntries = append(auxEntries, emitPyProtoSrc(ctx, instance, d, src, protocLDRef, protocBinary)...)
	}

	auxRes := emitPyProtoAuxChunks(ctx, instance, d, peerContribs, auxEntries)
	if auxRes != nil {
		pyProtoRefs = append(pyProtoRefs, auxRes.Refs...)
		pyProtoOutputs = append(pyProtoOutputs, auxRes.Outputs...)
	}
	if auxRes != nil && len(auxRes.MemberInputs) > 0 {
		pyProtoMemberInputs = append(pyProtoMemberInputs, pbPyWrapperVFS)
		pyProtoMemberInputs = append(pyProtoMemberInputs, auxRes.MemberInputs...)
	}

	if len(pyProtoRefs) == 0 {
		return nil
	}

	pyInstance := instance
	pyInstance.Language = LangPy
	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path, "libpy3", "")
	gRef := EmitARGlobalNamedTagged(pyInstance, globalBaseName, "py3_proto_global", pyProtoRefs, pyProtoOutputs, pyProtoMemberInputs, ctx.host, ctx.emit)

	globalPath := Build(instance.Path + "/" + globalBaseName)
	result := &protoSrcsResult{
		GlobalRef:  &gRef,
		GlobalPath: &globalPath,
	}
	if cppSibling != nil && cppSibling.ARPath != nil {
		if v, ok := ParseVFS(*cppSibling.ARPath); ok {
			result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
			result.WholeArchivePaths = append(result.WholeArchivePaths, strings.TrimPrefix(*cppSibling.ARPath, "$(B)/"))
			if d.optimizePyProtos {
				result.ARRef = cppSibling.ARRef
				result.ARPath = &v
			}
		}
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, ArchiveName(instance.Path))
		result.WholeArchiveCmdPaths[len(result.WholeArchiveCmdPaths)-1] = instance.Path + "/" + result.WholeArchiveCmdPaths[len(result.WholeArchiveCmdPaths)-1]
	}

	return result
}

func emitPyProtoSrc(ctx *genCtx, instance ModuleInstance, d *moduleData, src string, protocLDRef NodeRef, protocBinary string) []pyProtoAuxEntry {
	if d.moduleStmt == nil || d.moduleStmt.Name != "PROTO_LIBRARY" {
		return nil
	}

	protoRelPath := protoSourceRelPath(ctx.sourceRoot, instance, d, src)
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	protoRoot := protoPythonOutputRoot(instance, d)
	pyBase := protoBase + "__intpy3___pb2.py"
	pyOut := Build(pyBase)
	pyiOut := Build(protoBase + "__intpy3___pb2.pyi")
	var grpcPyOut VFS

	outputs := []VFS{pyOut}
	suffixes := []string{"_pb2.py"}
	if d.grpc {
		grpcPyOut = Build(protoBase + "__intpy3___pb2_grpc.py")
		outputs = append(outputs, grpcPyOut)
		suffixes = append(suffixes, "_pb2_grpc.py")
	}
	if !d.noMypy {
		outputs = append(outputs, pyiOut)
		suffixes = append(suffixes, "_pb2.pyi")
	}

	grpcPyBinary := pbGrpcPyPath
	mypyBinary := pbMypyPath
	var grpcPyRef, mypyRef NodeRef
	if d.grpc {
		grpcPyInst := NewToolInstance(ctx.host, pbGrpcPyModule)
		grpcPyInst.Flags = inferFlagsFromPath(pbGrpcPyModule, true)
		if exc := Try(func() {
			res := genModule(ctx, grpcPyInst)
			grpcPyRef = res.LDRef
			if res.LDPath != nil {
				grpcPyBinary = *res.LDPath
			}
		}); exc != nil {
			_ = exc
		}
	}
	if !d.noMypy {
		mypyInst := NewToolInstance(ctx.host, pbMypyModule)
		mypyInst.Flags = inferFlagsFromPath(pbMypyModule, true)
		if exc := Try(func() {
			res := genModule(ctx, mypyInst)
			mypyRef = res.LDRef
			if res.LDPath != nil {
				mypyBinary = *res.LDPath
			}
		}); exc != nil {
			_ = exc
		}
	}

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		pbPyWrapperPath,
		"--py_ver", "py3",
		"--suffixes",
	}
	cmdArgs = append(cmdArgs, suffixes...)
	cmdArgs = append(cmdArgs,
		"--input", protoRelPath,
		"--ns", protoPythonNamespaceArg(d),
		"--",
		protocBinary,
		"-I=./"+protoRoot,
		"-I=$(S)/"+protoRoot,
		"-I=$(B)",
		"-I=$(S)",
	)
	if protoRoot != "contrib/libs/protobuf/src" {
		cmdArgs = append(cmdArgs, "-I=$(S)/"+protoRoot)
	}
	cmdArgs = append(cmdArgs,
		"-I=$(S)/contrib/libs/protobuf/src",
		"-I=$(B)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"--python_out=$(B)/"+protoRoot,
		protoRelPath,
	)
	if d.grpc {
		cmdArgs = append(cmdArgs,
			"--plugin=protoc-gen-grpc_py="+grpcPyBinary,
			"--grpc_py_out=$(B)/"+protoRoot,
		)
	}
	if !d.noMypy {
		cmdArgs = append(cmdArgs,
			"--plugin=protoc-gen-mypy="+mypyBinary,
			"--mypy_out=$(B)/"+protoRoot,
		)
	}

	toolRefs := make([]NodeRef, 0, 3)
	if protocLDRef != (NodeRef{}) {
		toolRefs = append(toolRefs, protocLDRef)
	}
	if grpcPyRef != (NodeRef{}) {
		toolRefs = append(toolRefs, grpcPyRef)
	}
	if !d.noMypy && mypyRef != (NodeRef{}) {
		toolRefs = append(toolRefs, mypyRef)
	}

	inputs := []VFS{pbProtocBinaryVFS, pbPyWrapperVFS, Source(protoRelPath)}
	if protoImports := resolveProtoImports(ctx.sourceRoot, protoRelPath); protoImports != nil {
		if protoImports.HasDescriptor {
			inputs = append(inputs, pbDescriptorVFS)
		}
		inputs = append(inputs, protoImports.Imports...)
	}
	if d.grpc {
		inputs = append(inputs, pbGrpcPyVFS)
	}
	if !d.noMypy {
		inputs = append(inputs, pbMypyVFS)
	}

	pyPBNode := &Node{
		Cmds:             []Cmd{{CmdArgs: cmdArgs, Cwd: "$(S)", Env: map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}}},
		Env:              map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"},
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               map[string]string{"p": "PB", "pc": "yellow"},
		Tags:             instance.Platform.Tags,
		TargetProperties: map[string]string{"module_dir": instance.Path, "module_tag": "py3_proto"},
		Platform:         string(instance.Platform.Target),
		HostPlatform:     instance.Platform.IsHost,
		Requirements:     map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
		DepRefs:          toolRefs,
	}
	if len(toolRefs) > 0 {
		pyPBNode.ForeignDepRefs = map[string][]NodeRef{"tool": toolRefs}
	}
	pyPBRef := ctx.emit.Emit(pyPBNode)

	yapyRes := emitGeneratedPyProtoYapyc(ctx, instance, []VFS{pyOut, grpcPyOut}, pyPBRef)
	if yapyRes == nil {
		yapyRes = &generatedPyProtoYapycResult{}
	}
	return pyProtoAuxEntriesForSource(instance, d, src, pyPBRef, pyProtoSourceInputs(inputs), outputs, yapyRes.Refs, yapyRes.Outputs)
}

func protoPythonOutputRoot(instance ModuleInstance, d *moduleData) string {
	if d != nil && d.protoNamespace != nil {
		root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(*d.protoNamespace)), "/")
		if root != "." && root != "" {
			return root
		}
	}

	return instance.Path
}

type generatedPyProtoYapycResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitGeneratedPyProtoYapyc(ctx *genCtx, instance ModuleInstance, pyOutputs []VFS, pyPBRef NodeRef) *generatedPyProtoYapycResult {
	py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin := py3ccToolRefs(ctx, instance)
	suffix := protoPySuffix(instance.Path)

	res := &generatedPyProtoYapycResult{}
	for _, pyOut := range pyOutputs {
		if pyOut.Rel == "" {
			continue
		}

		out := Build(pyOut.Rel + "." + suffix + ".yapyc3")
		cmdArgs := []string{
			py3ccBinary,
			"--slow-py3cc",
			py3ccSlowBin,
			pyOut.Rel + "-",
			pyOut.String(),
			out.String(),
		}

		deps := []NodeRef{pyPBRef}
		var toolRefs []NodeRef
		if py3ccRef != (NodeRef{}) {
			deps = append(deps, py3ccRef)
			toolRefs = append(toolRefs, py3ccRef)
		}
		if py3ccSlowRef != (NodeRef{}) {
			deps = append(deps, py3ccSlowRef)
			toolRefs = append(toolRefs, py3ccSlowRef)
		}

		node := &Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)", "PYTHONHASHSEED": "0"}}},
			Env:              map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)", "PYTHONHASHSEED": "0"},
			Inputs:           []VFS{Build("tools/py3cc/py3cc"), Build("tools/py3cc/slow/py3cc"), pyOut},
			Outputs:          []VFS{out},
			KV:               map[string]string{"p": "PY", "pc": "yellow"},
			Tags:             []string{},
			TargetProperties: map[string]string{"module_dir": instance.Path, "module_tag": "py3_proto"},
			Platform:         string(instance.Platform.Target),
			HostPlatform:     instance.Platform.IsHost,
			Requirements:     map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
			DepRefs:          deps,
		}
		if len(toolRefs) > 0 {
			node.ForeignDepRefs = map[string][]NodeRef{"tool": toolRefs}
		}
		res.Refs = append(res.Refs, ctx.emit.Emit(node))
		res.Outputs = append(res.Outputs, out)
	}

	return res
}

type pyProtoAuxEntry struct {
	path     VFS
	key      string
	producer NodeRef
	inputs   []VFS
}

func pyProtoAuxEntriesForSource(instance ModuleInstance, d *moduleData, src string, pyPBRef NodeRef, producerInputs []VFS, pyOutputs []VFS, yapyRefs []NodeRef, yapyOuts []VFS) []pyProtoAuxEntry {
	var entries []pyProtoAuxEntry
	addResource := func(srcPath VFS, key string, producer NodeRef) {
		entries = append(entries, pyProtoAuxEntry{path: srcPath, key: key, producer: producer, inputs: producerInputs})
	}
	addResource(pyOutputs[0], protoPythonResourceKey(instance, d, src, "_pb2.py"), pyPBRef)
	if len(yapyOuts) > 0 {
		addResource(yapyOuts[0], protoPythonResourceKey(instance, d, src, "_pb2.py.yapyc3"), yapyRefs[0])
	}
	if d.grpc && len(pyOutputs) > 2 && pyOutputs[1].Rel != "" {
		addResource(pyOutputs[1], protoPythonResourceKey(instance, d, src, "_pb2_grpc.py"), pyPBRef)
		if len(yapyOuts) > 1 {
			addResource(yapyOuts[1], protoPythonResourceKey(instance, d, src, "_pb2_grpc.py.yapyc3"), yapyRefs[1])
		}
	}

	return entries
}

func pyProtoSourceInputs(inputs []VFS) []VFS {
	out := make([]VFS, 0, len(inputs))
	seen := map[VFS]struct{}{}
	for _, input := range inputs {
		if !input.IsSource() {
			continue
		}
		if _, ok := seen[input]; ok {
			continue
		}
		seen[input] = struct{}{}
		out = append(out, input)
	}
	return out
}

type pyProtoAuxChunksResult struct {
	Refs         []NodeRef
	Outputs      []VFS
	MemberInputs []VFS
}

func emitPyProtoAuxChunks(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, entries []pyProtoAuxEntry) *pyProtoAuxChunksResult {
	if len(entries) == 0 {
		return nil
	}

	rescompilerRef := walkHostToolForRef(ctx, instance, "tools/rescompiler/bin")
	type chunk struct {
		hashInputs []string
		cmdArgs    []string
		inputs     []VFS
		deps       []NodeRef
	}

	var chunks []chunk
	cur := chunk{}
	cmdLen := 0
	inputSeen := map[VFS]struct{}{}
	depSeen := map[NodeRef]struct{}{}

	addInput := func(v VFS) {
		if _, ok := inputSeen[v]; ok {
			return
		}
		inputSeen[v] = struct{}{}
		cur.inputs = append(cur.inputs, v)
	}
	addDep := func(ref NodeRef) {
		if ref == (NodeRef{}) {
			return
		}
		if _, ok := depSeen[ref]; ok {
			return
		}
		depSeen[ref] = struct{}{}
		cur.deps = append(cur.deps, ref)
	}
	flush := func() {
		if cmdLen == 0 {
			return
		}
		chunks = append(chunks, cur)
		cur = chunk{}
		cmdLen = 0
		inputSeen = map[VFS]struct{}{}
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.Rel
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.Rel

		cur.hashInputs = append(cur.hashInputs, "-", kvHash)
		cur.cmdArgs = append(cur.cmdArgs, "-", kvCmd)
		addInput(e.path)
		for _, input := range e.inputs {
			addInput(input)
		}
		addDep(e.producer)
		cmdLen += rootCmdLen + len("-") + len(kvHash)
		if cmdLen >= maxCmdLen {
			flush()
		}

		cur.hashInputs = append(cur.hashInputs, arcBuildPath, "-"+key)
		cur.cmdArgs = append(cur.cmdArgs, e.path.String(), "-"+key)
		addInput(e.path)
		for _, input := range e.inputs {
			addInput(input)
		}
		addDep(e.producer)
		cmdLen += rootCmdLen + len(arcBuildPath) + len(key)
		if cmdLen >= maxCmdLen {
			flush()
		}
	}
	flush()

	res := &pyProtoAuxChunksResult{}
	memberSeen := map[VFS]struct{}{}
	for _, ch := range chunks {
		aux := Build(instance.Path + "/" + protoResourceHash(ch.hashInputs, "$S/"+instance.Path, "PY3_PROTO") + "_raw.auxcpp")
		auxClosure := pyProtoAuxInputClosure(ctx, instance, d, peerContribs, aux, ch.inputs)
		cmdArgs := []string{rescompilerBinPath, aux.String()}
		cmdArgs = append(cmdArgs, ch.cmdArgs...)

		deps := append([]NodeRef(nil), ch.deps...)
		if rescompilerRef != (NodeRef{}) {
			deps = append(deps, rescompilerRef)
		}

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
		inputs := append([]VFS(nil), ch.inputs...)
		inputs = append(inputs, rescompilerBinVFS)
		inputs = append(inputs, auxClosure...)
		inputs = dedupVFS(inputs)
		ref := ctx.emit.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
			Env:              env,
			Inputs:           inputs,
			Outputs:          []VFS{aux},
			KV:               map[string]string{"p": "PR", "pc": "yellow", "show_out": "yes"},
			Tags:             instance.Platform.Tags,
			TargetProperties: map[string]string{"module_dir": instance.Path, "module_tag": "py3_proto"},
			Platform:         string(instance.Platform.Target),
			HostPlatform:     instance.Platform.IsHost,
			Requirements:     map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
			DepRefs:          deps,
		})

		ccIn := ModuleCCInputs{
			AddIncl:              pyProtoAuxOwnAddIncl(d),
			CFlags:               []string{"-DLZMA_API_STATIC", "-DOPENSSL_RENAME_SYMBOLS=1", "-DFFI_STATIC_BUILD", "-DUSE_PYTHON3"},
			PeerAddInclGlobal:    pyProtoAuxPeerAddIncl(instance, peerContribs, d),
			PeerCFlagsGlobal:     nil,
			PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
			PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
			AutoPeerCFlags:       []string{muslConsumerSentinel, "-DUSE_PYTHON3"},
			PerSourceCFlags:      []string{"-x", "c++"},
			SourceRoot:           ctx.sourceRoot,
			IsGenerated:          true,
			HasGenerator:         true,
			Generator:            ref,
			Py3Suffix:            true,
			ForceCxx:             true,
			ModuleTag:            stringPtr("py3_proto"),
			IncludeInputs:        auxClosure,
		}
		ccRef, ccOut := EmitCC(instance, aux.Rel[strings.LastIndex(aux.Rel, "/")+1:], ccIn, ctx.host, ctx.emit)
		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
		for _, v := range inputs {
			if _, ok := memberSeen[v]; ok {
				continue
			}
			memberSeen[v] = struct{}{}
			res.MemberInputs = append(res.MemberInputs, v)
		}
	}

	return res
}

func pyProtoAuxOwnAddIncl(d *moduleData) []string {
	out := make([]string, 0, 2)
	if d != nil && d.protoNamespace != nil {
		base := filepath.ToSlash(filepath.Clean(*d.protoNamespace))
		if base != "." && base != "" {
			out = append(out, "$(B)/"+base)
		}
	}
	out = append(out, "contrib/libs/python/Include")
	return out
}

func pyProtoAuxInputClosure(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, aux VFS, seed []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)
	if reg != nil {
		emits := []VFS{
			Source("library/cpp/resource/resource.h"),
			Source("library/cpp/resource/registry.h"),
		}
		for _, in := range seed {
			if in.IsSource() {
				emits = append(emits, in)
			}
		}
		emits = dedupVFS(emits)
		registerGeneratedOutput(ctx, instance, "PR", aux, emits)
	}

	scanIn := ModuleCCInputs{
		AddIncl:           pyProtoAuxOwnAddIncl(d),
		PeerAddInclGlobal: pyProtoAuxPeerAddIncl(instance, peerContribs, d),
		SourceRoot:        ctx.sourceRoot,
	}

	closure := walkClosure(ctx, instance, aux, scanIn)
	if len(closure) == 0 {
		return nil
	}

	return dedupVFS(closure)
}

func pyProtoAuxPeerAddIncl(instance ModuleInstance, peerContribs peerGlobalContribs, d *moduleData) []string {
	out := make([]string, 0, len(peerContribs.addIncl)+8)
	for _, p := range peerContribs.addIncl {
		if p == "contrib/libs/protobuf/src" || p == "contrib/restricted/abseil-cpp-tstring" || p == "contrib/restricted/abseil-cpp" {
			continue
		}
		out = append(out, p)
	}
	out = append(out,
		"contrib/libs/python/Include",
		"contrib/restricted/abseil-cpp",
		"$(B)/library/python/runtime_py3",
		"contrib/libs/lzma/liblzma/api",
		"contrib/libs/openssl/include",
		"contrib/restricted/libffi/include",
		"contrib/restricted/libffi/configs/"+libffiConfigTriple(instance.Platform)+"/include",
	)
	if d != nil && d.protoNamespace != nil {
		base := filepath.ToSlash(filepath.Clean(*d.protoNamespace))
		if base != "." && base != "" && base != "contrib/libs/protobuf/src" {
			out = append(out, "$(B)/contrib/libs/protobuf/src")
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

func py3ccToolRefs(ctx *genCtx, instance ModuleInstance) (NodeRef, NodeRef, string, string) {
	py3ccBinary := Build("tools/py3cc/py3cc").String()
	py3ccSlowBin := Build("tools/py3cc/slow/py3cc").String()
	var py3ccRef, py3ccSlowRef NodeRef

	inst := NewToolInstance(ctx.host, "tools/py3cc/bin")
	inst.Flags = inferFlagsFromPath("tools/py3cc/bin", true)
	if exc := Try(func() {
		res := genModule(ctx, inst)
		py3ccRef = res.LDRef
		if res.LDPath != nil {
			py3ccBinary = canonicalizePy3ccBinaryPath(*res.LDPath)
		}
	}); exc != nil {
		_ = exc
	}

	slowInst := NewToolInstance(ctx.host, "tools/py3cc/slow")
	slowInst.Flags = inferFlagsFromPath("tools/py3cc/slow", true)
	if exc := Try(func() {
		res := genModule(ctx, slowInst)
		py3ccSlowRef = res.LDRef
		if res.LDPath != nil {
			py3ccSlowBin = *res.LDPath
		}
	}); exc != nil {
		_ = exc
	}

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
