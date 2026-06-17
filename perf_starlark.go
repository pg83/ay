package main

import (
	"fmt"
	"time"
)

// cmdPerfStarlark benchmarks building the util module from ya.make vs ya.star, all over
// an in-memory FS with the two sources embedded below (utilYaMake / utilYaStar). It
// isolates the front-end cost (native parse vs Starlark eval) and the full module build
// (front-end + collectModule), each looped to a fixed wall-clock budget.
func cmdPerfStarlark(_ GlobalFlags, _ []string) int {
	defer startProfilesFromEnv()()

	return perfStarlark()
}

func perfStarlark() int {
	fs := newMemFS(map[string]string{
		"util/ya.make": utilYaMake,
		"util/ya.star": utilYaStar,
	})

	env := buildIfEnv(ModuleInstance{
		Path:     source("util"),
		Kind:     KindLib,
		Platform: newPlatform(fs, OSLinux, ISAX8664, map[string]string{"PIC": "no"}, "", ""),
	})

	collect := func(stmts []Stmt) {
		collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "util", KindLib, stmts, env)
	}

	parseMake := func() []Stmt { return throw2(parseFile(fs, "util/ya.make")).Stmts }
	evalStarFn := func() []Stmt { return throw2(evalStar(fs, "util/ya.star", env)) }

	// Warm up (and surface any error before timing).
	collect(parseMake())
	collect(evalStarFn())

	benchLoop("ya.make parse", func() { parseMake() })
	benchLoop("ya.star eval", func() { evalStarFn() })
	benchLoop("ya.make build", func() { collect(parseMake()) })
	benchLoop("ya.star build", func() { collect(evalStarFn()) })

	return 0
}

// benchLoop runs fn repeatedly for a fixed budget and prints iterations and per-op time.
func benchLoop(name string, fn func()) {
	const minDur = 2 * time.Second

	start := time.Now()
	iters := 0

	for time.Since(start) < minDur {
		fn()
		iters++
	}

	per := time.Since(start) / time.Duration(iters)
	fmt.Printf("%-16s iters=%-7d per-op=%v\n", name, iters, per)
}

const utilYaMake = `LIBRARY(yutil)

SUBSCRIBER(g:util g:util-subscribers)

NEED_CHECK()

NO_UTIL()

# stream
# string
PEERDIR(
    util/charset
    contrib/libs/zlib
    contrib/libs/double-conversion
)

PEERDIR(
    contrib/libs/libc_compat
)

# datetime
JOIN_SRCS(
    all_datetime.cpp
    datetime/base.cpp
    datetime/constants.cpp
    datetime/cputimer.cpp
    datetime/process_uptime.cpp
    datetime/systime.cpp
    datetime/uptime.cpp
)

SRCS(
    datetime/parser.rl6
    digest/city.cpp
    random/random.cpp
    string/cast.cpp
)

IF (OS_WINDOWS)
    SRCS(
        datetime/strptime.cpp
    )
ENDIF()

# digest
JOIN_SRCS(
    all_digest.cpp
    digest/fnv.cpp
    digest/multi.cpp
    digest/murmur.cpp
    digest/numeric.cpp
    digest/sequence.cpp
)

JOIN_SRCS(
    all_util.cpp
    ysafeptr.cpp
    ysaveload.cpp
    str_stl.cpp
)

# folder
JOIN_SRCS(
    all_folder.cpp
    folder/dirut.cpp
    folder/filelist.cpp
    folder/fts.cpp
    folder/fwd.cpp
    folder/iterator.cpp
    folder/path.cpp
    folder/pathsplit.cpp
    folder/tempdir.cpp
)

IF (OS_WINDOWS)
    SRCS(
        folder/lstat_win.c
        folder/dirent_win.c
    )
ENDIF()

# generic
JOIN_SRCS(
    all_generic.cpp
    generic/adaptor.cpp
    generic/algorithm.cpp
    generic/array_ref.cpp
    generic/array_size.cpp
    generic/bitmap.cpp
    generic/bitops.cpp
    generic/buffer.cpp
    generic/cast.cpp
    generic/deque.cpp
    generic/enum_cast.cpp
    generic/explicit_type.cpp
    generic/fastqueue.cpp
    generic/flags.cpp
    generic/function.cpp
    generic/function_ref.cpp
    generic/fwd.cpp
    generic/guid.cpp
    generic/hash.cpp
    generic/hash_multi_map.cpp
    generic/hash_table.cpp
    generic/hash_primes.cpp
    generic/hash_set.cpp
    generic/hide_ptr.cpp
    generic/intrlist.cpp
    generic/is_in.cpp
    generic/iterator.cpp
    generic/iterator_range.cpp
    generic/lazy_value.cpp
    generic/list.cpp
    generic/map.cpp
    generic/mapfindptr.cpp
    generic/maybe.cpp
    generic/mem_copy.cpp
    generic/noncopyable.cpp
    generic/object_counter.cpp
    generic/overloaded.cpp
    generic/ptr.cpp
    generic/queue.cpp
    generic/refcount.cpp
    generic/scope.cpp
    generic/serialized_enum.cpp
    generic/set.cpp
    generic/singleton.cpp
    generic/size_literals.cpp
    generic/stack.cpp
    generic/store_policy.cpp
    generic/strbuf.cpp
    generic/strfcpy.cpp
    generic/string.cpp
    generic/typelist.cpp
    generic/typetraits.cpp
    generic/utility.cpp
    generic/va_args.cpp
    generic/variant.cpp
    generic/vector.cpp
    generic/xrange.cpp
    generic/yexception.cpp
    generic/ylimits.cpp
    generic/ymath.cpp
)

# memory
JOIN_SRCS(
    all_memory.cpp
    memory/addstorage.cpp
    memory/alloc.cpp
    memory/blob.cpp
    memory/mmapalloc.cpp
    memory/pool.cpp
    memory/segmented_string_pool.cpp
    memory/segpool_alloc.cpp
    memory/smallobj.cpp
    memory/tempbuf.cpp
)

# network
JOIN_SRCS(
    all_network.cpp
    network/address.cpp
    network/endpoint.cpp
    network/hostip.cpp
    network/init.cpp
    network/interface.cpp
    network/iovec.cpp
    network/ip.cpp
    network/nonblock.cpp
    network/pair.cpp
    network/poller.cpp
    network/pollerimpl.cpp
    network/sock.cpp
    network/socket.cpp
)

# random
JOIN_SRCS(
    all_random.cpp
    random/common_ops.cpp
    random/easy.cpp
    random/entropy.cpp
    random/fast.cpp
    random/lcg_engine.cpp
    random/mersenne32.cpp
    random/mersenne64.cpp
    random/mersenne.cpp
    random/normal.cpp
    random/shuffle.cpp
    random/init_atfork.cpp
)

JOIN_SRCS(
    all_stream.cpp
    stream/aligned.cpp
    stream/buffer.cpp
    stream/buffered.cpp
    stream/direct_io.cpp
    stream/file.cpp
    stream/format.cpp
    stream/fwd.cpp
    stream/hex.cpp
    stream/holder.cpp
    stream/input.cpp
    stream/labeled.cpp
    stream/length.cpp
    stream/mem.cpp
    stream/multi.cpp
    stream/null.cpp
    stream/output.cpp
    stream/pipe.cpp
    stream/printf.cpp
    stream/str.cpp
    stream/tee.cpp
    stream/tempbuf.cpp
    stream/tokenizer.cpp
    stream/trace.cpp
    stream/walk.cpp
    stream/zerocopy.cpp
    stream/zerocopy_output.cpp
    stream/zlib.cpp
)

JOIN_SRCS(
    all_string.cpp
    string/ascii.cpp
    string/builder.cpp
    string/cstriter.cpp
    string/escape.cpp
    string/hex.cpp
    string/join.cpp
    string/printf.cpp
    string/reverse.cpp
    string/split.cpp
    string/strip.cpp
    string/strspn.cpp
    string/subst.cpp
    string/type.cpp
    string/util.cpp
    string/vector.cpp
)

IF (GCC OR CLANG OR CLANG_CL)
    CFLAGS(-Wnarrowing)
ENDIF()

IF (TSTRING_IS_STD_STRING)
    CFLAGS(GLOBAL -DTSTRING_IS_STD_STRING)
ENDIF()

IF (NO_CUSTOM_CHAR_PTR_STD_COMPARATOR)
    CFLAGS(GLOBAL -DNO_CUSTOM_CHAR_PTR_STD_COMPARATOR)
ENDIF()

JOIN_SRCS(
    all_system_1.cpp
    system/atexit.cpp
    system/backtrace.cpp
    system/compat.cpp
    system/condvar.cpp
    system/daemon.cpp
    system/datetime.cpp
    system/direct_io.cpp
    system/dynlib.cpp
    system/env.cpp
    system/error.cpp
    system/event.cpp
    system/fasttime.cpp
    system/file.cpp
    system/file_lock.cpp
    system/filemap.cpp
    system/flock.cpp
    system/fs.cpp
    system/fstat.cpp
    system/getpid.cpp
    system/hi_lo.cpp
    system/hostname.cpp
    system/hp_timer.cpp
    system/info.cpp
)
IF (NOT OS_EMSCRIPTEN)
JOIN_SRCS(
    all_system_2.cpp
    system/context.cpp
    system/execpath.cpp
)
ENDIF()

IF (OS_WINDOWS)
    SRCS(system/err.cpp)
ENDIF()

JOIN_SRCS(
    all_system_3.cpp
    system/align.cpp
    system/byteorder.cpp
    system/cpu_id.cpp
    system/fhandle.cpp
    system/guard.cpp
    system/interrupt_signals.cpp
    system/madvise.cpp
    system/maxlen.cpp
    system/mincore.cpp
    system/mktemp.cpp
    system/mlock.cpp
    system/mutex.cpp
    system/nice.cpp
    system/pipe.cpp
    system/platform.cpp
    system/progname.cpp
    system/protect.cpp
    system/rusage.cpp
    system/rwlock.cpp
    system/sanitizers.cpp
    system/shellcommand.cpp
    system/shmat.cpp
    system/sigset.cpp
    system/spinlock.cpp
    system/spin_wait.cpp
    system/src_location.cpp
    system/sys_alloc.cpp
    system/sysstat.cpp
    system/tempfile.cpp
    system/thread.cpp
    system/tls.cpp
    system/type_name.cpp
    system/unaligned_mem.cpp
    system/user.cpp
    system/utime.cpp
    system/yassert.cpp
    system/yield.cpp
)
IF (NOT OS_EMSCRIPTEN)
JOIN_SRCS(
    all_system_4.cpp
    system/mem_info.cpp
    system/sem.cpp
    system/types.cpp
)
ENDIF()

SRC_C_NO_LTO(system/compiler.cpp)

IF (OS_WINDOWS)
    SRCS(
        system/fs_win.cpp
        system/winint.cpp
    )
ELSEIF (OS_CYGWIN OR OS_IOS)
    # no asm context switching on cygwin or iOS
ELSE()
    IF (ARCH_X86_64 OR ARCH_I386)
        SRCS(
            system/context_x86.asm
        )
    ENDIF()
    IF (ARCH_AARCH64 OR ARCH_ARM64)
        SRCS(
            system/context_aarch64.S
        )
    ENDIF()
ENDIF()

IF (OS_LINUX)
    SRCS(
        system/valgrind.cpp
    )
    EXTRALIBS(
        -lrt
        -ldl
    )
ENDIF()

IF (MUSL)
    PEERDIR(
        contrib/libs/linuxvdso
    )
ELSE()
    IF (OS_LINUX OR SUN OR CYGWIN OR OS_WINDOWS)
        SRCS(
            system/mktemp_system.cpp
        )
    ENDIF()
ENDIF()

# thread
JOIN_SRCS(
    all_thread.cpp
    thread/factory.cpp
    thread/fwd.cpp
    thread/lfqueue.cpp
    thread/lfstack.cpp
    thread/pool.cpp
    thread/singleton.cpp
)

HEADERS(
    datetime
    digest
    folder
    generic
    memory
    network
    random
    stream
    string
    system
    thread
    EXCLUDE **/*_ut.h
)

END()

RECURSE(
    charset
    datetime
    digest
    draft
    folder
    generic
    memory
    network
    random
    stream
    string
    system
    thread
    ut
)
`

const utilYaStar = `# util/ya.star — declarative port of util/ya.make (Model A).
#
# Build flags are read through ` + "`" + `flags` + "`" + ` (e.g. flags.OS_WINDOWS == "yes"); the module's
# IF/ELSEIF/ELSE become ordinary Starlark conditionals that build the attribute lists.
# Generators (join_srcs, src_c_no_lto) are values composed into ` + "`" + `srcs` + "`" + `.


def on(v):
    return v == "yes"


peerdir = [
    "util/charset",
    "contrib/libs/zlib",
    "contrib/libs/double-conversion",
    "contrib/libs/libc_compat",
]

if on(flags.MUSL):
    peerdir += ["contrib/libs/linuxvdso"]


srcs = []

# datetime
srcs += join_srcs("all_datetime.cpp", [
    "datetime/base.cpp",
    "datetime/constants.cpp",
    "datetime/cputimer.cpp",
    "datetime/process_uptime.cpp",
    "datetime/systime.cpp",
    "datetime/uptime.cpp",
])
srcs += [
    "datetime/parser.rl6",
    "digest/city.cpp",
    "random/random.cpp",
    "string/cast.cpp",
]
if on(flags.OS_WINDOWS):
    srcs += ["datetime/strptime.cpp"]

# digest
srcs += join_srcs("all_digest.cpp", [
    "digest/fnv.cpp",
    "digest/multi.cpp",
    "digest/murmur.cpp",
    "digest/numeric.cpp",
    "digest/sequence.cpp",
])

srcs += join_srcs("all_util.cpp", [
    "ysafeptr.cpp",
    "ysaveload.cpp",
    "str_stl.cpp",
])

# folder
srcs += join_srcs("all_folder.cpp", [
    "folder/dirut.cpp",
    "folder/filelist.cpp",
    "folder/fts.cpp",
    "folder/fwd.cpp",
    "folder/iterator.cpp",
    "folder/path.cpp",
    "folder/pathsplit.cpp",
    "folder/tempdir.cpp",
])
if on(flags.OS_WINDOWS):
    srcs += [
        "folder/lstat_win.c",
        "folder/dirent_win.c",
    ]

# generic
srcs += join_srcs("all_generic.cpp", [
    "generic/adaptor.cpp",
    "generic/algorithm.cpp",
    "generic/array_ref.cpp",
    "generic/array_size.cpp",
    "generic/bitmap.cpp",
    "generic/bitops.cpp",
    "generic/buffer.cpp",
    "generic/cast.cpp",
    "generic/deque.cpp",
    "generic/enum_cast.cpp",
    "generic/explicit_type.cpp",
    "generic/fastqueue.cpp",
    "generic/flags.cpp",
    "generic/function.cpp",
    "generic/function_ref.cpp",
    "generic/fwd.cpp",
    "generic/guid.cpp",
    "generic/hash.cpp",
    "generic/hash_multi_map.cpp",
    "generic/hash_table.cpp",
    "generic/hash_primes.cpp",
    "generic/hash_set.cpp",
    "generic/hide_ptr.cpp",
    "generic/intrlist.cpp",
    "generic/is_in.cpp",
    "generic/iterator.cpp",
    "generic/iterator_range.cpp",
    "generic/lazy_value.cpp",
    "generic/list.cpp",
    "generic/map.cpp",
    "generic/mapfindptr.cpp",
    "generic/maybe.cpp",
    "generic/mem_copy.cpp",
    "generic/noncopyable.cpp",
    "generic/object_counter.cpp",
    "generic/overloaded.cpp",
    "generic/ptr.cpp",
    "generic/queue.cpp",
    "generic/refcount.cpp",
    "generic/scope.cpp",
    "generic/serialized_enum.cpp",
    "generic/set.cpp",
    "generic/singleton.cpp",
    "generic/size_literals.cpp",
    "generic/stack.cpp",
    "generic/store_policy.cpp",
    "generic/strbuf.cpp",
    "generic/strfcpy.cpp",
    "generic/string.cpp",
    "generic/typelist.cpp",
    "generic/typetraits.cpp",
    "generic/utility.cpp",
    "generic/va_args.cpp",
    "generic/variant.cpp",
    "generic/vector.cpp",
    "generic/xrange.cpp",
    "generic/yexception.cpp",
    "generic/ylimits.cpp",
    "generic/ymath.cpp",
])

# memory
srcs += join_srcs("all_memory.cpp", [
    "memory/addstorage.cpp",
    "memory/alloc.cpp",
    "memory/blob.cpp",
    "memory/mmapalloc.cpp",
    "memory/pool.cpp",
    "memory/segmented_string_pool.cpp",
    "memory/segpool_alloc.cpp",
    "memory/smallobj.cpp",
    "memory/tempbuf.cpp",
])

# network
srcs += join_srcs("all_network.cpp", [
    "network/address.cpp",
    "network/endpoint.cpp",
    "network/hostip.cpp",
    "network/init.cpp",
    "network/interface.cpp",
    "network/iovec.cpp",
    "network/ip.cpp",
    "network/nonblock.cpp",
    "network/pair.cpp",
    "network/poller.cpp",
    "network/pollerimpl.cpp",
    "network/sock.cpp",
    "network/socket.cpp",
])

# random
srcs += join_srcs("all_random.cpp", [
    "random/common_ops.cpp",
    "random/easy.cpp",
    "random/entropy.cpp",
    "random/fast.cpp",
    "random/lcg_engine.cpp",
    "random/mersenne32.cpp",
    "random/mersenne64.cpp",
    "random/mersenne.cpp",
    "random/normal.cpp",
    "random/shuffle.cpp",
    "random/init_atfork.cpp",
])

# stream
srcs += join_srcs("all_stream.cpp", [
    "stream/aligned.cpp",
    "stream/buffer.cpp",
    "stream/buffered.cpp",
    "stream/direct_io.cpp",
    "stream/file.cpp",
    "stream/format.cpp",
    "stream/fwd.cpp",
    "stream/hex.cpp",
    "stream/holder.cpp",
    "stream/input.cpp",
    "stream/labeled.cpp",
    "stream/length.cpp",
    "stream/mem.cpp",
    "stream/multi.cpp",
    "stream/null.cpp",
    "stream/output.cpp",
    "stream/pipe.cpp",
    "stream/printf.cpp",
    "stream/str.cpp",
    "stream/tee.cpp",
    "stream/tempbuf.cpp",
    "stream/tokenizer.cpp",
    "stream/trace.cpp",
    "stream/walk.cpp",
    "stream/zerocopy.cpp",
    "stream/zerocopy_output.cpp",
    "stream/zlib.cpp",
])

# string
srcs += join_srcs("all_string.cpp", [
    "string/ascii.cpp",
    "string/builder.cpp",
    "string/cstriter.cpp",
    "string/escape.cpp",
    "string/hex.cpp",
    "string/join.cpp",
    "string/printf.cpp",
    "string/reverse.cpp",
    "string/split.cpp",
    "string/strip.cpp",
    "string/strspn.cpp",
    "string/subst.cpp",
    "string/type.cpp",
    "string/util.cpp",
    "string/vector.cpp",
])

# system
srcs += join_srcs("all_system_1.cpp", [
    "system/atexit.cpp",
    "system/backtrace.cpp",
    "system/compat.cpp",
    "system/condvar.cpp",
    "system/daemon.cpp",
    "system/datetime.cpp",
    "system/direct_io.cpp",
    "system/dynlib.cpp",
    "system/env.cpp",
    "system/error.cpp",
    "system/event.cpp",
    "system/fasttime.cpp",
    "system/file.cpp",
    "system/file_lock.cpp",
    "system/filemap.cpp",
    "system/flock.cpp",
    "system/fs.cpp",
    "system/fstat.cpp",
    "system/getpid.cpp",
    "system/hi_lo.cpp",
    "system/hostname.cpp",
    "system/hp_timer.cpp",
    "system/info.cpp",
])
if not on(flags.OS_EMSCRIPTEN):
    srcs += join_srcs("all_system_2.cpp", [
        "system/context.cpp",
        "system/execpath.cpp",
    ])
if on(flags.OS_WINDOWS):
    srcs += ["system/err.cpp"]
srcs += join_srcs("all_system_3.cpp", [
    "system/align.cpp",
    "system/byteorder.cpp",
    "system/cpu_id.cpp",
    "system/fhandle.cpp",
    "system/guard.cpp",
    "system/interrupt_signals.cpp",
    "system/madvise.cpp",
    "system/maxlen.cpp",
    "system/mincore.cpp",
    "system/mktemp.cpp",
    "system/mlock.cpp",
    "system/mutex.cpp",
    "system/nice.cpp",
    "system/pipe.cpp",
    "system/platform.cpp",
    "system/progname.cpp",
    "system/protect.cpp",
    "system/rusage.cpp",
    "system/rwlock.cpp",
    "system/sanitizers.cpp",
    "system/shellcommand.cpp",
    "system/shmat.cpp",
    "system/sigset.cpp",
    "system/spinlock.cpp",
    "system/spin_wait.cpp",
    "system/src_location.cpp",
    "system/sys_alloc.cpp",
    "system/sysstat.cpp",
    "system/tempfile.cpp",
    "system/thread.cpp",
    "system/tls.cpp",
    "system/type_name.cpp",
    "system/unaligned_mem.cpp",
    "system/user.cpp",
    "system/utime.cpp",
    "system/yassert.cpp",
    "system/yield.cpp",
])
if not on(flags.OS_EMSCRIPTEN):
    srcs += join_srcs("all_system_4.cpp", [
        "system/mem_info.cpp",
        "system/sem.cpp",
        "system/types.cpp",
    ])

srcs += src_c_no_lto("system/compiler.cpp")

if on(flags.OS_WINDOWS):
    srcs += [
        "system/fs_win.cpp",
        "system/winint.cpp",
    ]
elif on(flags.OS_CYGWIN) or on(flags.OS_IOS):
    # no asm context switching on cygwin or iOS
    pass
else:
    if on(flags.ARCH_X86_64) or on(flags.ARCH_I386):
        srcs += ["system/context_x86.asm"]
    if on(flags.ARCH_AARCH64) or on(flags.ARCH_ARM64):
        srcs += ["system/context_aarch64.S"]

extralibs = []
if on(flags.OS_LINUX):
    srcs += ["system/valgrind.cpp"]
    extralibs += ["-lrt", "-ldl"]

if not on(flags.MUSL):
    if on(flags.OS_LINUX) or on(flags.SUN) or on(flags.CYGWIN) or on(flags.OS_WINDOWS):
        srcs += ["system/mktemp_system.cpp"]

# thread
srcs += join_srcs("all_thread.cpp", [
    "thread/factory.cpp",
    "thread/fwd.cpp",
    "thread/lfqueue.cpp",
    "thread/lfstack.cpp",
    "thread/pool.cpp",
    "thread/singleton.cpp",
])


cflags = []
if on(flags.GCC) or on(flags.CLANG) or on(flags.CLANG_CL):
    cflags += ["-Wnarrowing"]
if on(flags.TSTRING_IS_STD_STRING):
    cflags += ["GLOBAL", "-DTSTRING_IS_STD_STRING"]
if on(flags.NO_CUSTOM_CHAR_PTR_STD_COMPARATOR):
    cflags += ["GLOBAL", "-DNO_CUSTOM_CHAR_PTR_STD_COMPARATOR"]


library(
    "yutil",
    subscriber = ["g:util", "g:util-subscribers"],
    need_check = True,
    no_util = True,
    peerdir = peerdir,
    srcs = srcs,
    cflags = cflags,
    extralibs = extralibs,
    headers = [
        "datetime",
        "digest",
        "folder",
        "generic",
        "memory",
        "network",
        "random",
        "stream",
        "string",
        "system",
        "thread",
        "EXCLUDE",
        "**/*_ut.h",
    ],
)
`
