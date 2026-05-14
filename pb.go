package main

import (
	"bufio"
	"os"
	"path/filepath"
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
	pbProtocBinaryVFS  = Build("contrib/tools/protoc/protoc")
	pbCppStyleguideVFS = Build("contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide")
	pbDescriptorVFS    = Source("contrib/libs/protobuf/src/google/protobuf/descriptor.proto")

	pbWrapperPath       = pbWrapperVFS.String()
	pbProtocBinaryPath  = pbProtocBinaryVFS.String()
	pbCppStyleguidePath = pbCppStyleguideVFS.String()
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

// EmitPB emits a PB node for `srcRel` (a .proto file relative to `instance.Path`).
// `cppStyleguideLDRef` and `protocLDRef` are the host LD NodeRefs for the two
// tool programs (zeroed when the host walk failed). `cppStyleguideBinary` and
// `protocBinary` are the $(B)-rooted paths for the tool binaries.
// `moduleTag` is "cpp_proto" for PROTO_LIBRARY modules (may be empty for future use).
// `sourceRoot` is the absolute path to the source tree root (for descriptor-import scanning).
//
// Returns the emitted NodeRef.
func EmitPB(
	instance ModuleInstance,
	srcRel string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	cppStyleguideBinary string,
	protocBinary string,
	moduleTag string,
	sourceRoot string,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path
	protoRelPath := moduleDir + "/" + srcRel
	// Output paths strip the .proto suffix: foo.proto → foo.pb.h / foo.pb.cc.
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")
	srcVFS := Source(protoRelPath)

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		pbWrapperPath,
		"--outputs",
		pbH.String(),
		pbCC.String(),
		"--",
		protocBinary,
		"-I=./",
		"-I=$(S)/",
		"-I=$(B)",
		"-I=$(S)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"-I=$(B)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"--cpp_out=:$(B)/",
		"--cpp_styleguide_out=:$(B)/",
		"--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary,
		protoRelPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// inputs: [cpp_styleguide, protoc, wrapper, source, optionally descriptor.proto]
	inputs := []VFS{
		ParseVFSOrSource(cppStyleguideBinary),
		ParseVFSOrSource(protocBinary),
		pbWrapperVFS,
		srcVFS,
	}

	// If the source file imports "google/protobuf/descriptor.proto", add descriptor.proto.
	if protoImportsDescriptor(sourceRoot, moduleDir+"/"+srcRel) {
		inputs = append(inputs, pbDescriptorVFS)
	}

	// tags come from instance.Platform (["tool"] on host, [] on target);
	// non-nil empty slice keeps JSON `[]`, not `null`.
	tags := instance.Platform.Tags

	targetProps := map[string]string{
		"module_dir": moduleDir,
	}

	if moduleTag != "" {
		targetProps["module_tag"] = moduleTag
	}

	// deps and foreign_deps both carry the two tool refs.
	var depRefs []NodeRef
	var foreignDepRefs map[string][]NodeRef

	if cppStyleguideLDRef != (NodeRef{}) || protocLDRef != (NodeRef{}) {
		var toolRefs []NodeRef
		if cppStyleguideLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}
		if protocLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, protocLDRef)
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
		Outputs: []VFS{pbH, pbCC},
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

	if protoImportsDescriptor(sourceRoot, protoRelPath) {
		out = append(out, pbDescriptorVFS)
	}

	return out
}

// protoImportsDescriptor reports whether `<sourceRoot>/<srcRel>` imports
// "google/protobuf/descriptor.proto". Returns false when unreadable (no
// descriptor dep). Legitimate disk read scoped to PB-node-emission, not
// closure walks.
func protoImportsDescriptor(sourceRoot, srcRel string) bool {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)

	if err != nil {
		return false
	}

	defer f.Close()

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, `"google/protobuf/descriptor.proto"`) {
			return true
		}
	}

	return false
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
	ARRef  NodeRef
	ARPath VFS
}

func emitProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs) *protoSrcsResult {
	// Collect .proto and .ev sources from d.srcs.
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

	// Walk host protoc and cpp_styleguide tool programs.
	cppStyleguideBinary := pbCppStyleguidePath
	protocBinary := pbProtocBinaryPath

	var cppStyleguideLDRef, protocLDRef NodeRef

	protocHostInst := NewToolInstance(ctx.host, pbProtocModule, instance.Language)
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		protocBinary = result.LDPath
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	cppStyleguideHostInst := NewToolInstance(ctx.host, pbCppStyleguideModule, instance.Language)
	cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

	if exc := Try(func() {
		result := genModule(ctx, cppStyleguideHostInst)
		cppStyleguideLDRef = result.LDRef
		cppStyleguideBinary = result.LDPath
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
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

	// Emit PB nodes.
	for _, src := range protoSrcs {
		pbRef := EmitPB(
			instance, src, cppStyleguideLDRef, protocLDRef,
			cppStyleguideBinary, protocBinary,
			"cpp_proto", ctx.sourceRoot, ctx.emit)

		// Register the .pb.h with EmitsIncludes: .pb.h's of every imported
		// proto plus the constant protobuf runtime header set.
		protoRelPath := instance.Path + "/" + src
		protoBase := strings.TrimSuffix(protoRelPath, ".proto")
		pbH := Build(protoBase + ".pb.h")
		pbCC := Build(protoBase + ".pb.cc")

		// Stash the PB NodeRef under both output paths on the emitting
		// platform so resolveCodegenDepRefs can thread it as a direct dep
		// on consumer CCs whose IncludeInputs carry the .pb.h/.pb.cc path.
		// Keyed per-platform: x86_64 consumers reach the x86_64 PB,
		// aarch64 consumers reach the aarch64 PB.
		pbKey := codegenOutputKey{platform: instance.Platform.Target}
		pbKey.path = pbH
		ctx.pbOutputs[pbKey] = pbRef
		pbKey.path = pbCC
		ctx.pbOutputs[pbKey] = pbRef
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			directImports := protoDirectImportIncludes(ctx.sourceRoot, protoRelPath)
			extras := pbDescriptorImporterExtras(ctx.sourceRoot, protoRelPath)
			emitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(extras))
			emitsIncludes = append(emitsIncludes, directImports...)
			emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
			emitsIncludes = append(emitsIncludes, extras...)
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbH,
				EmitsIncludes: emitsIncludes,
			})
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
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbCC,
				EmitsIncludes: pbCCEmits,
			})
		}

		// Stash the (PB ref, .pb.cc, src-with-suffix) for downstream-CC + AR.
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  pbRef,
			pbCC:    pbCC,
			srcRel:  strings.TrimSuffix(src, ".proto") + ".pb.cc",
			primSrc: Source(protoRelPath),
		})
	}

	// Emit EV nodes (PROTO_LIBRARY with .ev sources → module_tag:"cpp_proto").
	if len(evSrcs) > 0 {
		event2cppBinary := evEvent2cppBinaryPath
		var event2cppLDRef NodeRef

		event2cppHostInst := NewToolInstance(ctx.host, evEvent2cppModule, instance.Language)
		event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, event2cppHostInst)
			event2cppLDRef = result.LDRef
			event2cppBinary = result.LDPath
		}); exc != nil {
			_ = exc
		}

		for _, src := range evSrcs {
			evRef := EmitEV(
				instance, src, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				"cpp_proto", ctx.sourceRoot, ctx.emit)

			// Register .ev.pb.h with EmitsIncludes: .ev source's direct
			// imports + protobuf runtime headers + EV-specific runtime
			// headers (util/* + eventlog).
			evRelPath := instance.Path + "/" + src
			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")

			// Stash the EV NodeRef under both outputs on the emitting
			// platform. See PB branch above for keying rationale.
			evKey := codegenOutputKey{platform: instance.Platform.Target}
			evKey.path = evH
			ctx.evOutputs[evKey] = evRef
			evKey.path = evPbCC
			ctx.evOutputs[evKey] = evRef
			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath)
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				emitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders)+len(evExtras))
				emitsIncludes = append(emitsIncludes, directImports...)
				emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, eventRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, evExtras...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evH,
					EmitsIncludes: emitsIncludes,
				})
				// Register .ev.pb.cc: event2cpp emits `#include
				// "<base>.ev.pb.h"` plus protobuf + event runtime headers.
				ccEmits := make([]VFS, 0, 1+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders))
				ccEmits = append(ccEmits, evH)
				ccEmits = append(ccEmits, protobufRuntimeHeaders...)
				ccEmits = append(ccEmits, eventRuntimeHeaders...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evPbCC,
					EmitsIncludes: ccEmits,
				})
			}

			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  evRef,
				pbCC:    evPbCC,
				srcRel:  src + ".pb.cc",
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
	// LibcMusl-self modules zero their own GLOBAL CFLAGS.
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if instance.Flags.LibcMusl {
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
		ModuleTag:            "cpp_proto",
	}

	// Per-source downstream-CC emission for the PROTO_LIBRARY context.
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

		ccRef, ccOut := EmitCC(instance, co.srcRel, ccIn, ctx.host, ctx.emit)
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
	arRef := emitARNode(instance, archivePath, "cpp_proto", ccRefs, ccOutputs, nil, memberInputs, nil, ctx.host, ctx.emit)
	return &protoSrcsResult{ARRef: arRef, ARPath: archivePath}
}
