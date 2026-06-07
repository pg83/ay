package main

import (
	"crypto/md5"
	"encoding/base32"
	enchex "encoding/hex"
	"path/filepath"
	"sort"
	"strings"
)

var (
	pbWrapperVFS      = Intern("$(S)/build/scripts/cpp_proto_wrapper.py")
	pbPyWrapperVFS    = Intern("$(S)/build/scripts/gen_py_protos.py")
	pbGrpcCppVFS      = Intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	pbDescriptorVFS   = Intern("$(S)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto")
	pbWrapperPath     = pbWrapperVFS.String()
	pbPyWrapperPath   = pbPyWrapperVFS.String()
	pbDescriptorProto = pbDescriptorVFS.String()
	// Path constants hoisted by `ay refac consts`.
	strLibraryCppResourceRegistryH = internString("library/cpp/resource/registry.h")
	strLibraryCppResourceResourceH = internString("library/cpp/resource/resource.h")
	// Path constants hoisted by `ay refac consts`.
	anyISYt = stringAny("-I=$(S)/yt")
)

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

var grpcServiceHeaderIncludes = []VFS{
	Intern("$(S)/contrib/libs/cxxsupp/libcxx/include/functional"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/client_context.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/completion_queue.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/generic/async_generic_service.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/proto_utils.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/rpc_method.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/server_callback_handlers.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/service_type.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/server_context.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/async_stream.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/async_unary_call.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/client_callback.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/message_allocator.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/method_handler.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/server_callback.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/status.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/stub_options.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/support/sync_stream.h"),
}

var grpcSourceExtraIncludes = []VFS{
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/channel_interface.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/client_unary_call.h"),
	Intern("$(S)/contrib/libs/grpc/include/grpcpp/impl/rpc_service_method.h"),
}

var pbDescriptorImporterHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/generated_message_bases.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_inl.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/reflection_ops.h"),
}

var pbCcDeepRuntimeHeaders = []VFS{
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

const (
	pbProtocModule        = "contrib/tools/protoc"
	pbCppStyleguideModule = "contrib/tools/protoc/plugins/cpp_styleguide"
	pbGrpcCppModule       = "contrib/tools/protoc/plugins/grpc_cpp"
	pbGrpcPyModule        = "contrib/tools/protoc/plugins/grpc_python"
	pbMypyModule          = "contrib/python/mypy-protobuf/bin/protoc-gen-mypy"

	pbRuntimeBase = "contrib/libs/protobuf/src/"

	abslTstringBase = "contrib/restricted/abseil-cpp-tstring/"
)

type resolvedCPPProtoPlugin struct {
	Spec   cppProtoPlugin
	LDRef  NodeRef
	Binary VFS
}

func EmitPB(
	instance ModuleInstance,
	protoRelPath string,
	protoSrcOverride VFS,
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
	liteHeaders bool,
	extraProtocFlags []ARG,
	extraPlugins []resolvedCPPProtoPlugin,
	transitiveProtoImports []VFS,
	hasDescriptor bool,
	peerProtoAddIncl []VFS,
	extraDepRefs []NodeRef,
	producerSourceInputs []VFS,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")
	pbDepsH := Build(protoBase + ".deps.pb.h")
	grpcPbCC := Build(protoBase + ".grpc.pb.cc")
	grpcPbH := Build(protoBase + ".grpc.pb.h")
	srcVFS := Source(protoRelPath)

	if protoSrcOverride != 0 {
		srcVFS = protoSrcOverride
	}

	outputs := []VFS{pbH, pbCC}

	if liteHeaders {
		outputs = append(outputs, pbDepsH)
	}

	if grpc {
		outputs = append(outputs, grpcPbCC, grpcPbH)
	}

	for _, plugin := range extraPlugins {
		for _, suffix := range plugin.Spec.OutputSuffixes {
			outputs = append(outputs, Build(protoBase+suffix))
		}
	}

	cmdArgs := []ANY{
		stringAny(instance.Platform.Tools.Python3),
		stringAny(pbWrapperPath),
		anyOutputs,
	}

	for _, output := range outputs {
		cmdArgs = append(cmdArgs, vfsAny(output))
	}

	includeRoot := ""

	if cppOutRoot != "" {
		includeRoot = cppOutRoot
	}

	cppOutArg := ":$(B)/" + cppOutRoot

	if liteHeaders {
		cppOutArg = "proto_h=true" + cppOutArg
	}

	cmdArgs = append(cmdArgs,
		any2,
		vfsAny(protocBinary),
		stringAny("-I=./"+includeRoot),
		stringAny("-I=$(S)/"+includeRoot),
		anyIB2,
		anyIS3,
	)

	if cppOutRoot != "" {
		cmdArgs = append(cmdArgs, stringAny("-I=$(S)/"+cppOutRoot))

		if duplicateOutputRootInclude {
			cmdArgs = append(cmdArgs, stringAny("-I=$(S)/"+cppOutRoot))
		}
	}

	// Upstream's _CPP_PROTO_CMDLINE_BASE (ymake.core.conf:612) emits
	// `${pre=-I=:_PROTO__INCLUDE} -I=$ARCADIA_BUILD_ROOT
	// -I=$PROTOBUF_INCLUDE_PATH` — peers first, then $(B), then protobuf-src.
	// _PROTO__INCLUDE already contains protobuf-src for LIBRARY modules that
	// transitively peer contrib/libs/protobuf (its ya.make declares
	// `ADDINCL GLOBAL FOR proto contrib/libs/protobuf/src`), so the protobuf
	// -I shows up via the peer loop AS WELL AS via the trailing macro
	// expansion. PROTO_LIBRARY filters peers to CPP_PROTO-tagged modules
	// (proto.conf:921), so contrib/libs/protobuf's FOR proto addincl does
	// NOT enter its peer chain — only PROTO_LIBRARY-internal protos
	// (which need it via `ADDINCL GLOBAL FOR proto contrib/libs/protobuf/src`
	// from their own peers).
	for _, p := range peerProtoAddIncl {
		cmdArgs = append(cmdArgs, stringAny("-I="+p.String()))
	}

	if moduleTag == nil && strings.HasPrefix(protoRelPath, "yt/") {
		cmdArgs = append(cmdArgs, anyISYt)
	}

	cmdArgs = append(cmdArgs,
		anyIB2,
		anyISContribLibsProtobufSrc,
		stringAny("--cpp_out="+cppOutArg),
	)
	cmdArgs = appendArgAny(cmdArgs, extraProtocFlags)
	cmdArgs = append(cmdArgs,
		stringAny("--cpp_styleguide_out=:$(B)/"+cppOutRoot),
		stringAny("--plugin=protoc-gen-cpp_styleguide="+cppStyleguideBinary.String()),
		stringAny(protoRelPath),
	)

	if grpc {
		cmdArgs = append(cmdArgs,
			stringAny("--plugin=protoc-gen-grpc_cpp="+grpcCppBinary.String()),
			stringAny("--grpc_cpp_out=$(B)/"+cppOutRoot),
		)
	}

	for _, plugin := range extraPlugins {
		cmdArgs = append(cmdArgs,
			stringAny("--plugin=protoc-gen-"+plugin.Spec.Name+"="+plugin.Binary.String()),
			stringAny("--"+plugin.Spec.Name+"_out=$(B)/"+cppOutRoot),
		)

		if plugin.Spec.ExtraOutFlag != "" {
			cmdArgs = append(cmdArgs, stringAny("--"+plugin.Spec.Name+"_opt=:"+plugin.Spec.ExtraOutFlag))
		}
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := []VFS{
		cppStyleguideBinary,
	}

	if grpc {
		inputs = append(inputs, grpcCppBinary)
	}

	inputs = append(inputs, protocBinary)

	for _, plugin := range extraPlugins {
		inputs = append(inputs, plugin.Binary)
	}

	inputs = append(inputs, pbWrapperVFS)

	if hasDescriptor {
		inputs = append(inputs, pbDescriptorVFS)
	}

	inputs = append(inputs, srcVFS)
	inputs = append(inputs, transitiveProtoImports...)
	// When srcVFS is build-generated, carry the producer's transitive $(S) leaf
	// sources (e.g. the RUN_ANTLR grammar / template / jar / scripts behind a
	// generated .proto) so the PB node's input set matches upstream's flat
	// source closure.
	inputs = append(inputs, producerSourceInputs...)

	tags := instance.Platform.Tags

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != nil {
		targetProps.ModuleTag = *moduleTag
	}

	var depRefs []NodeRef
	var foreignDepRefs []NodeRef

	if cppStyleguideLDRef != (NodeRef(0)) || protocLDRef != (NodeRef(0)) || grpcCppLDRef != (NodeRef(0)) || len(extraPlugins) > 0 {
		var toolRefs []NodeRef

		if cppStyleguideLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}

		if grpcCppLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, grpcCppLDRef)
		}

		if protocLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, protocLDRef)
		}

		for _, plugin := range extraPlugins {
			if plugin.LDRef == (NodeRef(0)) {
				continue
			}

			toolRefs = append(toolRefs, plugin.LDRef)
		}

		depRefs = append([]NodeRef(nil), toolRefs...)
		foreignDepRefs = toolRefs
	}

	// Producer refs for build-generated proto sources (e.g. RUN_ANTLR -lang
	// protobuf): without these the producer JV is unreachable from the LD
	// root closure and gets DFS-pruned at finalize.
	depRefs = append(depRefs, extraDepRefs...)

	// A build-generated .proto (protoSrcOverride set) lives under $(B); protoc
	// runs from $(B) so its relative `-I=./` and the proto path resolve to the
	// generated tree. Source .protos run from $(S).
	protocCwd := "$(S)"

	if protoSrcOverride != 0 {
		protocCwd = "$(B)"
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     protocCwd,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkPB, PC: pcYellow},
		Tags:             tags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))
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

type protoSrcsResult struct {
	ARRef                NodeRef
	ARPath               *VFS
	GlobalRef            *NodeRef
	GlobalPath           *VFS
	WholeArchiveRefs     []NodeRef
	WholeArchivePaths    []VFS
	WholeArchiveCmdPaths []VFS
}

func protoSourceRelPath(fs FS, instance ModuleInstance, d *moduleData, src string) string {
	moduleRel := filepath.ToSlash(filepath.Clean(instance.Path + "/" + src))

	if fs.IsFile(dirKey(instance.Path), src) {
		return moduleRel
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

func pyProtoAuxInputClosure(ctx *genCtx, instance ModuleInstance, d *moduleData, aux VFS, seed []VFS, peerAddIncl []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)

	if reg != nil {
		emits := []includeDirective{
			{kind: includeQuoted, target: strLibraryCppResourceResourceH},
			{kind: includeQuoted, target: strLibraryCppResourceRegistryH},
		}

		for _, in := range seed {
			if in.IsSource() {
				emits = append(emits, includeDirective{kind: includeQuoted, target: internString(in.Rel())})
			}
		}

		registerGeneratedParsedOutput(ctx, instance, "PR", aux, emits)
	}

	scanIn := ModuleCCInputs{
		InclArgs:          ctx.inclArgs,
		Flags:             d.flags,
		AddIncl:           d.addIncl,
		PeerAddInclGlobal: peerAddIncl,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	closure := walkClosure(ctx, instance, aux, scanIn)

	if len(closure) == 0 {
		return nil
	}

	// walkClosure already returns a deduplicated window — no further dedup needed.
	return closure
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
