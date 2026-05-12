package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// pb.go — emitter for PB (Protocol Buffers compile) nodes.
//
// EmitPB emits one PB node per .proto source in a PROTO_LIBRARY.
// Each node invokes cpp_proto_wrapper.py (a Python wrapper) which
// calls protoc with the cpp_styleguide plugin. The wrapper and both
// tool binaries come from contrib/tools/protoc (host programs).
//
// Reference shape (18 cmd_args, verified against sg2.json):
//
//	/ix/realm/pg/bin/python3
//	$(SOURCE_ROOT)/build/scripts/cpp_proto_wrapper.py
//	--outputs <.pb.h> <.pb.cc>
//	--
//	$(BUILD_ROOT)/contrib/tools/protoc/protoc
//	-I=./ -I=$(SOURCE_ROOT)/ -I=$(BUILD_ROOT) -I=$(SOURCE_ROOT)
//	-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src
//	-I=$(BUILD_ROOT) -I=$(SOURCE_ROOT)/contrib/libs/protobuf/src
//	--cpp_out=:$(BUILD_ROOT)/
//	--cpp_styleguide_out=:$(BUILD_ROOT)/
//	--plugin=protoc-gen-cpp_styleguide=<cpp_styleguide_binary>
//	<module_dir/proto_file>
//
// inputs = [cpp_styleguide_binary, protoc_binary, cpp_proto_wrapper.py,
//           $(SOURCE_ROOT)/<module_dir>/<src>,
//           optionally $(SOURCE_ROOT)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto]
//
// descriptor.proto is included in inputs when the .proto source imports
// "google/protobuf/descriptor.proto" (detected by scanning the source
// file for that import string).
//
// foreign_deps / deps both carry [cpp_styleguide_LD_ref, protoc_LD_ref]
// (two tool refs; the order matches the reference graph's uid list).
//
// tags: ["tool"] when platform == x86_64 (host build), [] otherwise.
// target_properties: module_dir (always) + module_tag:"cpp_proto" (always).

const (
	pbPython3Path       = "/ix/realm/pg/bin/python3"
	pbWrapperPath       = "$(SOURCE_ROOT)/build/scripts/cpp_proto_wrapper.py"
	pbProtocBinaryPath  = "$(BUILD_ROOT)/contrib/tools/protoc/protoc"
	pbCppStyleguidePath = "$(BUILD_ROOT)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
	pbDescriptorProto   = "$(SOURCE_ROOT)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto"

	// Tool module paths for host-walk recursion.
	pbProtocModule        = "contrib/tools/protoc"
	pbCppStyleguideModule = "contrib/tools/protoc/plugins/cpp_styleguide"

	// pbRuntimeBase is the $(SOURCE_ROOT)-rooted prefix for all protobuf
	// runtime headers (under contrib/libs/protobuf/src/).
	pbRuntimeBase = "$(SOURCE_ROOT)/contrib/libs/protobuf/src/"

	// abslTstringBase is the $(SOURCE_ROOT)-rooted prefix for abseil-cpp-tstring
	// headers. The protobuf runtime transitively reaches a large
	// abseil-cpp-tstring closure via `port_def.inc → y_absl/strings/string_view.h
	// → ...`; consumer PROTO_LIBRARYs do not peer abseil-cpp-tstring themselves
	// (it is an internal protobuf-runtime dependency), so the scanner cannot
	// resolve `y_absl/...` includes without pre-resolved EmitsIncludes.
	abslTstringBase = "$(SOURCE_ROOT)/contrib/restricted/abseil-cpp-tstring/"
)

// protobufRuntimeHeaders is the set of headers that every protoc-generated
// .pb.h directly #includes (verified by reading any.pb.h, duration.pb.h,
// timestamp.pb.h, etc.). These are registered as EmitsIncludes on the .pb.h
// output so the scanner closure propagates them into all CC nodes that
// include the .pb.h. Scanner recursion then finds their transitive includes.
// Sorted lexicographically. VFS-rooted $(SOURCE_ROOT)/... paths.
var protobufRuntimeHeaders = []string{
	pbRuntimeBase + "google/protobuf/arena.h",
	pbRuntimeBase + "google/protobuf/arenastring.h",
	pbRuntimeBase + "google/protobuf/extension_set.h",
	pbRuntimeBase + "google/protobuf/generated_message_reflection.h",
	pbRuntimeBase + "google/protobuf/generated_message_util.h",
	pbRuntimeBase + "google/protobuf/io/coded_stream.h",
	pbRuntimeBase + "google/protobuf/message.h",
	pbRuntimeBase + "google/protobuf/metadata_lite.h",
	pbRuntimeBase + "google/protobuf/port_def.inc",
	pbRuntimeBase + "google/protobuf/port_undef.inc",
	pbRuntimeBase + "google/protobuf/repeated_field.h",
	pbRuntimeBase + "google/protobuf/unknown_field_set.h",
}

// pbDescriptorImporterHeaders are the protobuf runtime headers that appear in
// CC consumers of any .pb.h whose source proto imports
// "google/protobuf/descriptor.proto". These pull in the
// map/reflection_ops cluster that protoc emits in the reflection metadata for
// extension-bearing protos (verified by intersecting the inputs of every
// descriptor.proto-importing .pb.h's CC consumer in sg2.json — see
// docs/drafts/20260512-0200-residue-pre-100pct.md §2 lever #1).
// Sorted lexicographically.
var pbDescriptorImporterHeaders = []string{
	pbRuntimeBase + "google/protobuf/generated_message_bases.h",
	pbRuntimeBase + "google/protobuf/map_entry.h",
	pbRuntimeBase + "google/protobuf/map_entry_lite.h",
	pbRuntimeBase + "google/protobuf/map_field.h",
	pbRuntimeBase + "google/protobuf/map_field_inl.h",
	pbRuntimeBase + "google/protobuf/map_field_lite.h",
	pbRuntimeBase + "google/protobuf/reflection_ops.h",
}

// pbCcDeepRuntimeHeaders is the deep protobuf+abseil transitive header set that
// every protoc-generated .pb.cc transitively reaches via #include closure.
// Verified by diffing the REF inputs of `library/cpp/eventlog/proto/internal.pb.cc.o`
// (and four sibling .pb.cc.o nodes) against our emitted set.
//
// PR-M3-proto-abseil-pb-cc-closure: registered as EmitsIncludes on the .pb.cc
// output ONLY — NOT on the .pb.h. The .pb.h is consumed by every CC node that
// includes any .pb.h (broad), whereas the .pb.cc is consumed by exactly the
// single CC compile node for that .pb.cc.o (narrow). Registering on .pb.h
// over-emits these headers onto 100+ non-protobuf CC consumers (the prior
// reverted commit 870d43d caused L2 -1.05pp via that path). Registering on
// .pb.cc keeps the closure scoped to the .pb.cc.o consumer alone.
//
// The set spans two groups:
//
//  1. The deep protobuf transitive set reached via port_def.inc and the core
//     message/runtime chain — descriptor.h, parse_context.h, map.h,
//     wire_format*.h, stubs/*, etc. (42 entries).
//  2. The full 145-entry abseil-cpp-tstring transitive closure reached via
//     port_def.inc → y_absl/strings/string_view.h → ... The per-file libcxx
//     #includes inside each y_absl header resolve through the consumer's own
//     libcxx language-default peer (scanner DFS walks parseIncludes for
//     SOURCE_ROOT paths, so libcxx <vector>/<string>/... discovery is
//     automatic once the abseil headers are walkable).
//
// Sorted lexicographically. VFS-rooted $(SOURCE_ROOT)/... paths.
var pbCcDeepRuntimeHeaders = []string{
	// Group 1: deep protobuf transitive set.
	pbRuntimeBase + "google/protobuf/any.h",
	pbRuntimeBase + "google/protobuf/arena_align.h",
	pbRuntimeBase + "google/protobuf/arena_allocation_policy.h",
	pbRuntimeBase + "google/protobuf/arena_cleanup.h",
	pbRuntimeBase + "google/protobuf/arena_config.h",
	pbRuntimeBase + "google/protobuf/arenaz_sampler.h",
	pbRuntimeBase + "google/protobuf/descriptor.h",
	pbRuntimeBase + "google/protobuf/endian.h",
	pbRuntimeBase + "google/protobuf/explicitly_constructed.h",
	pbRuntimeBase + "google/protobuf/generated_enum_reflection.h",
	pbRuntimeBase + "google/protobuf/generated_enum_util.h",
	pbRuntimeBase + "google/protobuf/generated_message_bases.h",
	pbRuntimeBase + "google/protobuf/generated_message_tctable_decl.h",
	pbRuntimeBase + "google/protobuf/has_bits.h",
	pbRuntimeBase + "google/protobuf/implicit_weak_message.h",
	pbRuntimeBase + "google/protobuf/inlined_string_field.h",
	pbRuntimeBase + "google/protobuf/io/zero_copy_stream.h",
	pbRuntimeBase + "google/protobuf/io/zero_copy_stream_impl.h",
	pbRuntimeBase + "google/protobuf/io/zero_copy_stream_impl_lite.h",
	pbRuntimeBase + "google/protobuf/json_util.h",
	pbRuntimeBase + "google/protobuf/map.h",
	pbRuntimeBase + "google/protobuf/map_entry.h",
	pbRuntimeBase + "google/protobuf/map_entry_lite.h",
	pbRuntimeBase + "google/protobuf/map_field.h",
	pbRuntimeBase + "google/protobuf/map_field_inl.h",
	pbRuntimeBase + "google/protobuf/map_field_lite.h",
	pbRuntimeBase + "google/protobuf/map_type_handler.h",
	pbRuntimeBase + "google/protobuf/message_lite.h",
	pbRuntimeBase + "google/protobuf/messagext.h",
	pbRuntimeBase + "google/protobuf/parse_context.h",
	pbRuntimeBase + "google/protobuf/port.h",
	pbRuntimeBase + "google/protobuf/reflection_ops.h",
	pbRuntimeBase + "google/protobuf/repeated_ptr_field.h",
	pbRuntimeBase + "google/protobuf/serial_arena.h",
	pbRuntimeBase + "google/protobuf/string_block.h",
	pbRuntimeBase + "google/protobuf/stubs/callback.h",
	pbRuntimeBase + "google/protobuf/stubs/common.h",
	pbRuntimeBase + "google/protobuf/stubs/platform_macros.h",
	pbRuntimeBase + "google/protobuf/stubs/port.h",
	pbRuntimeBase + "google/protobuf/thread_safe_arena.h",
	pbRuntimeBase + "google/protobuf/wire_format.h",
	pbRuntimeBase + "google/protobuf/wire_format_lite.h",

	// Group 2: abseil-cpp-tstring deep transitive closure reached from
	// port_def.inc → string_view.h → ... (145 entries).
	abslTstringBase + "y_absl/algorithm/algorithm.h",
	abslTstringBase + "y_absl/algorithm/container.h",
	abslTstringBase + "y_absl/base/attributes.h",
	abslTstringBase + "y_absl/base/call_once.h",
	abslTstringBase + "y_absl/base/casts.h",
	abslTstringBase + "y_absl/base/config.h",
	abslTstringBase + "y_absl/base/const_init.h",
	abslTstringBase + "y_absl/base/dynamic_annotations.h",
	abslTstringBase + "y_absl/base/internal/atomic_hook.h",
	abslTstringBase + "y_absl/base/internal/dynamic_annotations.h",
	abslTstringBase + "y_absl/base/internal/endian.h",
	abslTstringBase + "y_absl/base/internal/errno_saver.h",
	abslTstringBase + "y_absl/base/internal/identity.h",
	abslTstringBase + "y_absl/base/internal/inline_variable.h",
	abslTstringBase + "y_absl/base/internal/invoke.h",
	abslTstringBase + "y_absl/base/internal/low_level_alloc.h",
	abslTstringBase + "y_absl/base/internal/low_level_scheduling.h",
	abslTstringBase + "y_absl/base/internal/nullability_impl.h",
	abslTstringBase + "y_absl/base/internal/per_thread_tls.h",
	abslTstringBase + "y_absl/base/internal/raw_logging.h",
	abslTstringBase + "y_absl/base/internal/scheduling_mode.h",
	abslTstringBase + "y_absl/base/internal/spinlock.h",
	abslTstringBase + "y_absl/base/internal/spinlock_wait.h",
	abslTstringBase + "y_absl/base/internal/thread_identity.h",
	abslTstringBase + "y_absl/base/internal/throw_delegate.h",
	abslTstringBase + "y_absl/base/internal/tsan_mutex_interface.h",
	abslTstringBase + "y_absl/base/internal/unaligned_access.h",
	abslTstringBase + "y_absl/base/log_severity.h",
	abslTstringBase + "y_absl/base/macros.h",
	abslTstringBase + "y_absl/base/nullability.h",
	abslTstringBase + "y_absl/base/optimization.h",
	abslTstringBase + "y_absl/base/options.h",
	abslTstringBase + "y_absl/base/policy_checks.h",
	abslTstringBase + "y_absl/base/port.h",
	abslTstringBase + "y_absl/base/prefetch.h",
	abslTstringBase + "y_absl/base/thread_annotations.h",
	abslTstringBase + "y_absl/container/btree_map.h",
	abslTstringBase + "y_absl/container/fixed_array.h",
	abslTstringBase + "y_absl/container/flat_hash_map.h",
	abslTstringBase + "y_absl/container/hash_container_defaults.h",
	abslTstringBase + "y_absl/container/inlined_vector.h",
	abslTstringBase + "y_absl/container/internal/btree.h",
	abslTstringBase + "y_absl/container/internal/btree_container.h",
	abslTstringBase + "y_absl/container/internal/common.h",
	abslTstringBase + "y_absl/container/internal/common_policy_traits.h",
	abslTstringBase + "y_absl/container/internal/compressed_tuple.h",
	abslTstringBase + "y_absl/container/internal/container_memory.h",
	abslTstringBase + "y_absl/container/internal/hash_function_defaults.h",
	abslTstringBase + "y_absl/container/internal/hash_policy_traits.h",
	abslTstringBase + "y_absl/container/internal/hashtable_debug_hooks.h",
	abslTstringBase + "y_absl/container/internal/hashtablez_sampler.h",
	abslTstringBase + "y_absl/container/internal/inlined_vector.h",
	abslTstringBase + "y_absl/container/internal/layout.h",
	abslTstringBase + "y_absl/container/internal/raw_hash_map.h",
	abslTstringBase + "y_absl/container/internal/raw_hash_set.h",
	abslTstringBase + "y_absl/crc/crc32c.h",
	abslTstringBase + "y_absl/crc/internal/crc32_x86_arm_combined_simd.h",
	abslTstringBase + "y_absl/crc/internal/crc32c_inline.h",
	abslTstringBase + "y_absl/crc/internal/crc_cord_state.h",
	abslTstringBase + "y_absl/debugging/internal/demangle.h",
	abslTstringBase + "y_absl/functional/any_invocable.h",
	abslTstringBase + "y_absl/functional/function_ref.h",
	abslTstringBase + "y_absl/functional/internal/any_invocable.h",
	abslTstringBase + "y_absl/functional/internal/function_ref.h",
	abslTstringBase + "y_absl/hash/hash.h",
	abslTstringBase + "y_absl/hash/internal/city.h",
	abslTstringBase + "y_absl/hash/internal/hash.h",
	abslTstringBase + "y_absl/hash/internal/low_level_hash.h",
	abslTstringBase + "y_absl/log/absl_check.h",
	abslTstringBase + "y_absl/log/absl_log.h",
	abslTstringBase + "y_absl/log/absl_vlog_is_on.h",
	abslTstringBase + "y_absl/log/internal/check_impl.h",
	abslTstringBase + "y_absl/log/internal/check_op.h",
	abslTstringBase + "y_absl/log/internal/conditions.h",
	abslTstringBase + "y_absl/log/internal/config.h",
	abslTstringBase + "y_absl/log/internal/log_impl.h",
	abslTstringBase + "y_absl/log/internal/log_message.h",
	abslTstringBase + "y_absl/log/internal/nullguard.h",
	abslTstringBase + "y_absl/log/internal/nullstream.h",
	abslTstringBase + "y_absl/log/internal/proto.h",
	abslTstringBase + "y_absl/log/internal/strip.h",
	abslTstringBase + "y_absl/log/internal/structured_proto.h",
	abslTstringBase + "y_absl/log/internal/vlog_config.h",
	abslTstringBase + "y_absl/log/internal/voidify.h",
	abslTstringBase + "y_absl/log/log_entry.h",
	abslTstringBase + "y_absl/log/log_sink.h",
	abslTstringBase + "y_absl/memory/memory.h",
	abslTstringBase + "y_absl/meta/type_traits.h",
	abslTstringBase + "y_absl/numeric/bits.h",
	abslTstringBase + "y_absl/numeric/int128.h",
	abslTstringBase + "y_absl/numeric/int128_have_intrinsic.inc",
	abslTstringBase + "y_absl/numeric/int128_no_intrinsic.inc",
	abslTstringBase + "y_absl/numeric/internal/bits.h",
	abslTstringBase + "y_absl/profiling/internal/sample_recorder.h",
	abslTstringBase + "y_absl/strings/cord.h",
	abslTstringBase + "y_absl/strings/cord_analysis.h",
	abslTstringBase + "y_absl/strings/cord_buffer.h",
	abslTstringBase + "y_absl/strings/has_absl_stringify.h",
	abslTstringBase + "y_absl/strings/internal/cord_data_edge.h",
	abslTstringBase + "y_absl/strings/internal/cord_internal.h",
	abslTstringBase + "y_absl/strings/internal/cord_rep_btree.h",
	abslTstringBase + "y_absl/strings/internal/cord_rep_btree_navigator.h",
	abslTstringBase + "y_absl/strings/internal/cord_rep_btree_reader.h",
	abslTstringBase + "y_absl/strings/internal/cord_rep_crc.h",
	abslTstringBase + "y_absl/strings/internal/cord_rep_flat.h",
	abslTstringBase + "y_absl/strings/internal/cordz_functions.h",
	abslTstringBase + "y_absl/strings/internal/cordz_handle.h",
	abslTstringBase + "y_absl/strings/internal/cordz_info.h",
	abslTstringBase + "y_absl/strings/internal/cordz_statistics.h",
	abslTstringBase + "y_absl/strings/internal/cordz_update_scope.h",
	abslTstringBase + "y_absl/strings/internal/cordz_update_tracker.h",
	abslTstringBase + "y_absl/strings/internal/resize_uninitialized.h",
	abslTstringBase + "y_absl/strings/internal/str_format/arg.h",
	abslTstringBase + "y_absl/strings/internal/str_format/bind.h",
	abslTstringBase + "y_absl/strings/internal/str_format/checker.h",
	abslTstringBase + "y_absl/strings/internal/str_format/constexpr_parser.h",
	abslTstringBase + "y_absl/strings/internal/str_format/extension.h",
	abslTstringBase + "y_absl/strings/internal/str_format/output.h",
	abslTstringBase + "y_absl/strings/internal/str_format/parser.h",
	abslTstringBase + "y_absl/strings/internal/string_constant.h",
	abslTstringBase + "y_absl/strings/internal/stringify_sink.h",
	abslTstringBase + "y_absl/strings/numbers.h",
	abslTstringBase + "y_absl/strings/str_cat.h",
	abslTstringBase + "y_absl/strings/str_format.h",
	abslTstringBase + "y_absl/strings/string_view.h",
	abslTstringBase + "y_absl/synchronization/internal/create_thread_identity.h",
	abslTstringBase + "y_absl/synchronization/internal/kernel_timeout.h",
	abslTstringBase + "y_absl/synchronization/internal/per_thread_sem.h",
	abslTstringBase + "y_absl/synchronization/mutex.h",
	abslTstringBase + "y_absl/time/civil_time.h",
	abslTstringBase + "y_absl/time/clock.h",
	abslTstringBase + "y_absl/time/internal/cctz/include/cctz/civil_time.h",
	abslTstringBase + "y_absl/time/internal/cctz/include/cctz/civil_time_detail.h",
	abslTstringBase + "y_absl/time/internal/cctz/include/cctz/time_zone.h",
	abslTstringBase + "y_absl/time/time.h",
	abslTstringBase + "y_absl/types/bad_optional_access.h",
	abslTstringBase + "y_absl/types/bad_variant_access.h",
	abslTstringBase + "y_absl/types/compare.h",
	abslTstringBase + "y_absl/types/internal/optional.h",
	abslTstringBase + "y_absl/types/internal/span.h",
	abslTstringBase + "y_absl/types/internal/variant.h",
	abslTstringBase + "y_absl/types/optional.h",
	abslTstringBase + "y_absl/types/span.h",
	abslTstringBase + "y_absl/types/variant.h",
	abslTstringBase + "y_absl/utility/utility.h",
}

// EmitPB emits a PB node for `srcRel` (a .proto file relative to `instance.Path`).
// `cppStyleguideLDRef` and `protocLDRef` are the host LD NodeRefs for the two
// tool programs (zeroed when the host walk failed). `cppStyleguideBinary` and
// `protocBinary` are the $(BUILD_ROOT)-rooted paths for the tool binaries.
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

	pbH := "$(BUILD_ROOT)/" + protoBase + ".pb.h"
	pbCC := "$(BUILD_ROOT)/" + protoBase + ".pb.cc"
	srcAbs := "$(SOURCE_ROOT)/" + protoRelPath

	cmdArgs := []string{
		pbPython3Path,
		pbWrapperPath,
		"--outputs",
		pbH,
		pbCC,
		"--",
		protocBinary,
		"-I=./",
		"-I=$(SOURCE_ROOT)/",
		"-I=$(BUILD_ROOT)",
		"-I=$(SOURCE_ROOT)",
		"-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src",
		"-I=$(BUILD_ROOT)",
		"-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src",
		"--cpp_out=:$(BUILD_ROOT)/",
		"--cpp_styleguide_out=:$(BUILD_ROOT)/",
		"--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary,
		protoRelPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// inputs: [cpp_styleguide, protoc, wrapper, source, optionally descriptor.proto]
	inputs := []string{
		cppStyleguideBinary,
		protocBinary,
		pbWrapperPath,
		srcAbs,
	}

	// If the source file imports "google/protobuf/descriptor.proto", add descriptor.proto.
	if protoImportsDescriptor(sourceRoot, moduleDir+"/"+srcRel) {
		inputs = append(inputs, pbDescriptorProto)
	}

	// tags: ["tool"] on host (x86_64), [] on target.
	tags := []string{}
	if targetIsX8664(instance) {
		tags = []string{"tool"}
	}

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
				Cwd:     "$(SOURCE_ROOT)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  inputs,
		Outputs: []string{pbH, pbCC},
		KV: map[string]string{
			"p":  "PB",
			"pc": "yellow",
		},
		Tags:             tags,
		TargetProperties: targetProps,
		Platform: string(instance.Target),
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

// pbDescriptorImporterExtras returns the witness inputs propagated through a
// protoc-generated .pb.h to its CC consumers. The list is the union of:
//   - pbDescriptorImporterHeaders (7 protobuf reflection-cluster headers),
//   - pbWrapperPath (cpp_proto_wrapper.py — the script that drives protoc),
//   - the proto source file (its $(SOURCE_ROOT)-rooted path),
//   - pbDescriptorProto (the descriptor.proto source itself; only when the
//     proto imports descriptor.proto).
//
// Verified by intersecting CC-consumer inputs across all .pb.h's in
// /home/pg/monorepo/yatool_orig/sg2.json: the 7 headers + wrapper + proto
// source appear on 105/105 cpp.o consumers, regardless of descriptor
// import (PR-M3-final-codegen-registry-expansion).
func pbDescriptorImporterExtras(sourceRoot, protoRelPath string) []string {
	out := make([]string, 0, len(pbDescriptorImporterHeaders)+3)
	out = append(out, pbWrapperPath)
	out = append(out, "$(SOURCE_ROOT)/"+protoRelPath)
	out = append(out, pbDescriptorImporterHeaders...)

	if protoImportsDescriptor(sourceRoot, protoRelPath) {
		out = append(out, pbDescriptorProto)
	}

	return out
}

// protoImportsDescriptor reports whether the .proto (or .ev) source file at
// `<sourceRoot>/<srcRel>` contains an import of "google/protobuf/descriptor.proto".
// Returns false when the file cannot be read (missing source → no descriptor dep).
//
// PR-AUDIT-3: legitimate disk read — extracts a single structured `import`
// predicate from a .proto/.ev source at PB-node-emission time. NOT for closure
// walks. The architectural cleanup to route this through a unified
// registry-resolved "structured-import extractor" lives in PR-AUDIT-3.D12
// (still open); kept per audit doc §2 D12.
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

// emitProtoSrcs emits PB/EV nodes for .proto and .ev entries in d.srcs
// when the module is a PROTO_LIBRARY. Called from the header-only
// branch of genModule after peer-walking, before returning the result.
// PB/EV emitters walk the host protoc + cpp_styleguide tool instances to
// get their LDRefs; the same cached instances are shared across all
// PROTO_LIBRARY modules via the genCtx memo.
//
// PR-M3-proto-library-ar: for true PROTO_LIBRARY modules (module name
// `PROTO_LIBRARY`), after emitting PB/EV nodes, this function ALSO
// emits the downstream CC for each generated .pb.cc / .ev.pb.cc and an
// AR archiving them into `lib<dotted-path>.a` with module_tag=cpp_proto.
// Mirrors the LIBRARY/EV branch in `gen.go::emitOneSource` (the .ev
// case at line 4315) for the per-source downstream-CC dispatch; mirrors
// the LIBRARY AR shape at line 3097 for the archive step.
// `peerContribs` carries the transitive per-axis peer-GLOBAL union the
// header-only walker computed (used to compose the per-CC ModuleCCInputs
// so flags reach the consumer CCs of the protoc-generated sources).
//
// PR-M3-LD-peer-globalA: returns (arRef, arPath, true) when a PROTO_LIBRARY
// AR was emitted so the caller can surface it through `moduleEmitResult`'s
// archive closure (the AR was previously orphaned — emitted as a graph
// node but not reachable from any LD `inputs` via the peer walk).
func emitProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs) (NodeRef, string, bool) {
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
		return NodeRef{}, "", false
	}

	// Walk host protoc and cpp_styleguide tool programs.
	cppStyleguideBinary := pbCppStyleguidePath
	protocBinary := pbProtocBinaryPath

	var cppStyleguideLDRef, protocLDRef NodeRef

	protocHostInst := instance.WithHost(ctx.cfg)
	protocHostInst.Path = pbProtocModule
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		protocBinary = result.LDPath
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	cppStyleguideHostInst := instance.WithHost(ctx.cfg)
	cppStyleguideHostInst.Path = pbCppStyleguideModule
	cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

	if exc := Try(func() {
		result := genModule(ctx, cppStyleguideHostInst)
		cppStyleguideLDRef = result.LDRef
		cppStyleguideBinary = result.LDPath
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	// PR-M3-proto-library-ar: collect per-codegen-source (genRef, .pb.cc path)
	// pairs so the AR step can fold them into ccRefs/ccOutputs/memberInputs
	// in declaration order. Mirrors the LIBRARY AR aggregation pattern
	// (gen.go:2761 addMemberInputs(ccIns) inside the per-source loop).
	type protoCodegenOutput struct {
		genRef NodeRef // PB or EV node ref (used as Generator dep for the downstream CC)
		pbCC   string  // generated .pb.cc / .ev.pb.cc absolute BUILD_ROOT path
		srcRel string  // module-relative source-with-codegen-suffix (".pb.cc" appended)
		primSrc string // primary source path ($(SOURCE_ROOT)/<module>/<src>) for AR memberInputs
	}

	var codegenOutputs []protoCodegenOutput

	// Emit PB nodes.
	for _, src := range protoSrcs {
		pbRef := EmitPB(instance, src, cppStyleguideLDRef, protocLDRef,
			cppStyleguideBinary, protocBinary,
			"cpp_proto", ctx.sourceRoot, ctx.emit)

		// F-7-B: register the .pb.h output with its EmitsIncludes. The .pb.h
		// includes the .pb.h of every proto imported by this source, plus the
		// constant protobuf runtime header set (F-7-D).
		protoRelPath := instance.Path + "/" + src
		protoBase := strings.TrimSuffix(protoRelPath, ".proto")
		pbH := "$(BUILD_ROOT)/" + protoBase + ".pb.h"
		pbCC := "$(BUILD_ROOT)/" + protoBase + ".pb.cc"
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			directImports := protoDirectImportIncludes(ctx.sourceRoot, protoRelPath)
			extras := pbDescriptorImporterExtras(ctx.sourceRoot, protoRelPath)
			emitsIncludes := make([]string, 0, len(directImports)+len(protobufRuntimeHeaders)+len(extras))
			emitsIncludes = append(emitsIncludes, directImports...)
			emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
			emitsIncludes = append(emitsIncludes, extras...)
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbH,
				EmitsIncludes: emitsIncludes,
			})
			// PR-AUDIT-6 step 4: register the .pb.cc output too. protoc emits a
			// `#include "<base>.pb.h"` plus the protobuf runtime headers; the
			// .pb.h's own EmitsIncludes are already registered (just above), so a
			// single entry pointing at the .pb.h would suffice — we mirror the
			// .pb.h list for symmetry with the LIBRARY/EV path (gen.go:4338-4342).
			//
			// PR-M3-proto-abseil-pb-cc-closure: the .pb.cc.o consumer also reaches
			// the deep protobuf+abseil-cpp-tstring transitive closure that
			// port_def.inc opens (witnessed in REF for every PROTO_LIBRARY's
			// .pb.cc.o; see pbCcDeepRuntimeHeaders for the 187-entry list).
			// Plus REF includes the .proto source itself and the cpp_proto_wrapper.py
			// script in every PROTO_LIBRARY consumer's .pb.cc.o inputs (verified
			// against `library/cpp/eventlog/proto/internal.pb.cc.o`). Scope is
			// narrow: these headers register on the .pb.cc output ONLY, NOT the
			// .pb.h — the .pb.h is consumed by 100+ broad CC nodes which must NOT
			// inherit the abseil closure (over-emission regression in reverted
			// commit 870d43d cost L2 -1.05pp). The .pb.cc is consumed by exactly
			// one CC compile node, so the closure is tightly scoped.
			pbCCEmits := make([]string, 0, 3+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders))
			pbCCEmits = append(pbCCEmits, pbH)
			pbCCEmits = append(pbCCEmits, "$(SOURCE_ROOT)/"+protoRelPath)
			pbCCEmits = append(pbCCEmits, pbWrapperPath)
			pbCCEmits = append(pbCCEmits, protobufRuntimeHeaders...)
			pbCCEmits = append(pbCCEmits, pbCcDeepRuntimeHeaders...)
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbCC,
				EmitsIncludes: pbCCEmits,
			})
		}

		// PR-M3-proto-library-ar: stash the (PB ref, .pb.cc, src-with-suffix)
		// for the downstream-CC + AR step below.
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  pbRef,
			pbCC:    pbCC,
			srcRel:  strings.TrimSuffix(src, ".proto") + ".pb.cc",
			primSrc: "$(SOURCE_ROOT)/" + protoRelPath,
		})
	}

	// Emit EV nodes (PROTO_LIBRARY with .ev sources → module_tag:"cpp_proto").
	if len(evSrcs) > 0 {
		event2cppBinary := evEvent2cppBinaryPath
		var event2cppLDRef NodeRef

		event2cppHostInst := instance.WithHost(ctx.cfg)
		event2cppHostInst.Path = evEvent2cppModule
		event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, event2cppHostInst)
			event2cppLDRef = result.LDRef
			event2cppBinary = result.LDPath
		}); exc != nil {
			_ = exc
		}

		for _, src := range evSrcs {
			evRef := EmitEV(instance, src, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				"cpp_proto", ctx.sourceRoot, ctx.emit)

			// F-7-B: register the .ev.pb.h output with EmitsIncludes derived from
			// the .ev source's direct imports, plus the protobuf runtime headers (F-7-D)
			// and the EV-specific runtime headers (util/* + eventlog).
			evRelPath := instance.Path + "/" + src
			evH := "$(BUILD_ROOT)/" + evRelPath + ".pb.h"
			evPbCC := "$(BUILD_ROOT)/" + evRelPath + ".pb.cc"
			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath)
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				emitsIncludes := make([]string, 0, len(directImports)+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders)+len(evExtras))
				emitsIncludes = append(emitsIncludes, directImports...)
				emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, eventRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, evExtras...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evH,
					EmitsIncludes: emitsIncludes,
				})
				// PR-AUDIT-6 step 4: register the .ev.pb.cc output too. event2cpp
				// emits a `#include "<base>.ev.pb.h"` plus the protobuf + event
				// runtime headers; mirror the .pb.h list for symmetry with the
				// LIBRARY/EV path (gen.go:4338-4342).
				ccEmits := make([]string, 0, 1+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders))
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
				primSrc: "$(SOURCE_ROOT)/" + evRelPath,
			})
		}
	}

	// PR-M3-proto-library-ar: for true PROTO_LIBRARY modules, emit the
	// downstream CC for each generated .pb.cc / .ev.pb.cc and the AR
	// archiving them. Skip for non-PROTO_LIBRARY callers — the LIBRARY
	// path's own .ev branch in emitOneSource already handles its own
	// downstream-CC + AR aggregation (gen.go:4315).
	if d.moduleStmt.Name != "PROTO_LIBRARY" || len(codegenOutputs) == 0 {
		return NodeRef{}, "", false
	}

	// Compose ModuleCCInputs for the downstream CCs. Mirror the LIBRARY
	// path's moduleInputs construction (gen.go:2632) but pull the per-axis
	// peer-GLOBAL slices from the header-only walker's peerContribs.
	// LibcMusl-self modules zero their own GLOBAL CFLAGS (mirror of
	// gen.go:1925-1929 in the header-only branch).
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

	// Per-source downstream-CC emission. Mirrors gen.go:4399-4411 (EV
	// LIBRARY branch) but for the PROTO_LIBRARY context.
	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]string, 0, len(codegenOutputs))
	memberInputs := make([]string, 0, 64)
	memberInputsSeen := make(map[string]struct{})

	addMemberInputs := func(paths []string) {
		for _, p := range paths {
			if _, dup := memberInputsSeen[p]; dup {
				continue
			}
			memberInputsSeen[p] = struct{}{}
			memberInputs = append(memberInputs, p)
		}
	}

	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IsGenerated = true
		ccIn.Generator = co.genRef
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = walkClosure(ctx, instance, co.pbCC, moduleInputs)
		// PR-M3-final-codegen-registry-expansion: protoc emits
		// `#include "google/protobuf/wire_format.h"` into the reflection
		// scaffolding of every .ev.pb.cc; add it post-closure (registry-side
		// addition leaks through .ev.pb.h → .ev.pb.cc witness link to
		// over-emit onto non-protobuf CC nodes). Restricted to .ev sources
		// (.pb.cc gets wire_format.h via pbCcDeepRuntimeHeaders already).
		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, pbRuntimeBase+"google/protobuf/wire_format.h")
		}

		ccRef, ccOut := EmitCC(instance, co.srcRel, ccIn, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)

		// AR memberInputs: primary source first, then the CC's include closure.
		// Mirror of gen.go:4414-4415 (LIBRARY EV branch returning the .ev
		// source as the primary member input) + gen.go:2761 addMemberInputs.
		perCC := make([]string, 0, 1+len(ccIn.IncludeInputs))
		perCC = append(perCC, co.primSrc)
		perCC = append(perCC, ccIn.IncludeInputs...)
		addMemberInputs(perCC)
	}

	// AR emission. Mirrors gen.go:3097 EmitARNamed with module_tag=cpp_proto.
	arBaseName := ArchiveName(instance.Path)
	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + arBaseName
	arRef := emitARNode(instance, archivePath, "cpp_proto", ccRefs, ccOutputs, nil, memberInputs, ctx.emit)
	return arRef, archivePath, true
}
