# T-4 — HAVE_MKL platform/config variable evaluation

## Symptom
sg7 (linux x86_64, internal contour) over-emits the fallback
`contrib/libs/clapack`, `contrib/libs/cblas`, `contrib/libs/libf2c`
compile/archive producer family. The upstream reference instead selects the
Intel MKL branch (`PEERDIR(contrib/libs/intel/mkl)`, MKL include path,
`LINK_MKL_STATICALLY`). The BLAS/LAPACK contrib `ya.make` files
(`contrib/libs/clapack/ya.make`, `contrib/libs/cblas/ya.make`) branch via
`IF (HAVE_MKL) PEERDIR(contrib/libs/intel/mkl) ELSE <source build> ENDIF`.

## Upstream mechanism
`build/ymake.core.conf:373`:
```
HAVE_MKL=
when ($HAVE_MKL == "") {
    when ($OS_LINUX && $ARCH_X86_64 && !$SANITIZER_TYPE) { HAVE_MKL=yes }
    otherwise { HAVE_MKL=no }
}
```
`build/conf/opensource.conf:19` forces `HAVE_MKL=no` inside
`when ($OPENSOURCE == "yes")`.

So HAVE_MKL = yes iff (OS_LINUX && ARCH_X86_64 && SANITIZER_TYPE=="") and the
contour is not opensource; otherwise no. Our IF environment never binds
HAVE_MKL, so `IF (HAVE_MKL)` reads it as unset/false and always takes the
fallback ELSE branch — the source-build clapack/cblas/libf2c family.

## Fix
Reproduce the two upstream steps **in their upstream order** in `buildIfEnv`
(the per-module IF-env builder, `modules.go`), where the analogous
OPENSOURCE-derived bindings (YA_OPENSOURCE, CATBOOST_OPENSOURCE, _USE_AIO/...)
already live and where ARCH_X86_64 is set from the platform ISA. After the ISA
switch:

1. `ymake.core.conf:373` default guard — if not already bound by Platform.Flags
   (ya.conf): HAVE_MKL = OS_LINUX && ARCH_X86_64 && SANITIZER_TYPE=="". A
   pre-existing binding wins, matching `when ($HAVE_MKL == "")`.
2. `opensource.conf:19` override — if OPENSOURCE: HAVE_MKL=no, applied
   **unconditionally after** step 1, so it overrides even an explicit
   HAVE_MKL=yes flag (the upstream assignment is not guarded by the empty
   check; it is a later, unconditional statement under `when ($OPENSOURCE)`).

The earlier rework collapsed both into a single `!hasBindingID` guard, which
made an existing HAVE_MKL binding suppress the OPENSOURCE override — an
OPENSOURCE=yes platform carrying HAVE_MKL=yes would have kept MKL enabled,
diverging from upstream. Splitting the two statements restores the upstream
ordering.

Add `envHAVE_MKL` to env_consts.go.

## Files
- `env_consts.go`: add `envHAVE_MKL`.
- `modules.go` (`buildIfEnv`): bind HAVE_MKL per the rule above.
- `modules_test.go`: regression — an `IF (HAVE_MKL)` module selects the MKL
  PEERDIR on linux/x86_64 non-opensource and the fallback PEERDIR on
  aarch64 / sanitizer / opensource. Includes an OPENSOURCE=yes platform that
  also carries an explicit HAVE_MKL=yes flag, proving the opensource.conf:19
  override beats the existing binding (forced no).

## Expected effect on the gate
sg7 no longer emits the fallback clapack/cblas/libf2c producers for this
branch; the MKL branch is selected as upstream. sg2–sg6 are byte-exact
(opensource snapshots → HAVE_MKL=no, identical branch selection to the prior
unset→false). `go test ./...` passes; validate.py gating counts do not drop.
