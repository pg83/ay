# Style Guide

## Error handling

All error handling goes through `throw.go` — a thin panic/recover wrapper that turns Go's two-value error returns into exception-style flow.

### Primitives

- `Throw(err error)` — if `err != nil`, panic with an `*Exception`.
- `Throw2[T](val T, err error) T` — unwraps `(val, err)` returns; re-throws on error, otherwise returns `val`.
- `Throw3[T1,T2](v1 T1, v2 T2, err error) (T1, T2)` — three-value version.
- `ThrowFmt(format, args...)` — like `Throw(fmt.Errorf(...))` but unconditional; for raising our own errors.
- `Fmt(format, args...) *Exception` — construct an exception without throwing.
- `Try(cb func()) *Exception` — catch-all. Runs `cb`, converts `*Exception` panics into returned values, lets any other panic propagate.
- `(*Exception).Catch(cb)` — if non-nil, call `cb` with it. Fluent error handler at the boundary.
- `(*Exception).AsError() error` — interoperate with stdlib/3rd-party APIs that want an `error`.

### Rule

**No `if err != nil { return err }` in application code.** Wrap calls in `Throw2`/`Throw` instead:

```go
// BAD
f, err := os.Open(path)
if err != nil {
    return err
}

// GOOD
f := Throw2(os.Open(path))
```

Catches belong at boundaries:

- `main.go`: the top-level `Try(func(){...}).Catch(...)` prints the error and `os.Exit(1)`.
- Each goroutine's entry function: wrap the body in `Try(...)` and log via `Catch` — otherwise a panic escapes the goroutine and kills the process.
- Any filter-style loop where a per-iteration failure should be skipped, not propagated: wrap the iteration body in `Try` and ignore.

### When `if err != nil` is allowed

Local, non-propagating uses are fine:

- **Filter**: `if err != nil { continue }` to skip a bad item in a loop (e.g. iterating `/proc` entries).
- **Discriminate**: checking `errors.As` to recognise an expected case (e.g. `*exec.ExitError` to extract an exit code, or an S3 404 to decide "not yet uploaded").

The forbidden shape is a pure **pass-through** — `if err != nil { return err }` that does nothing except re-type the bubble. Use `Throw` for that.

### When a function should return `error`

Returning an `error` is fine — even encouraged — when the error is **part of the function's contract** and the caller is expected to branch on it, not just propagate it further. Typical cases:

- **Interface obligation**: e.g. `flag.Value.Set(string) error`, `io.Reader.Read`, `json.Unmarshaler.UnmarshalJSON`. The stdlib/3rd-party contract requires a returned `error`; don't wrap it in `Throw` to spite the signature.
- **Domain signal that drives a branch**: the error is an outcome the caller discriminates on (often via `errors.As`/`errors.Is` or a typed/sentinel error). Example: `lookupUser` returning `ErrNotFound` so the caller can decide whether to create the user or fail. The error is data, not a bubble.
- **Expected failure at a boundary, with actionable next step**: functions exposed to external integration (RPC handlers, CLI library callbacks) may need to return an error in a particular shape so the surrounding framework can format, log, or transport it.

The distinction is: does the caller do something *specific* with this error, or does it just `return err`? If the latter — you're passing through, use `Throw`. If the former — return the error; the value carries meaning.

## Formatting

### Blank lines around control blocks

Before and after `if`, `for`, `switch`, `select`, `go func`, `defer func` — add a blank line.

Exception: no blank line if the block is the first or last statement inside `{}`.

```go
func foo() {
    if cond {              // first stmt, no blank before
        return
    }
                           // blank after
    doThing()
                           // blank before for
    for _, x := range xs {
        use(x)
    }
}                          // switch was last stmt, no blank after
```

### Blank lines before `return`

Always add a blank line before `return`.

Exception: no blank line if `return` is the first statement after `{`.

```go
func empty() int {
    return 0               // first stmt, no blank
}

func nonEmpty() int {
    x := compute()
                           // blank before return
    return x
}
```

### Logical grouping

Consecutive one-liners (`Throw*`, `defer`, `:=`, `=`) that form a single logical operation stay together without blank lines. Between separate logical operations — add a blank line.

Example — opening a file and deferring close is one operation:

```go
f := Throw2(os.Open(path))
defer f.Close()

socksAddrs := parseProxyFile(f)  // next operation
```

Example — setting up a resource is one operation, using it is another:

```go
dev := Throw2(netlink.LinkByName(device))
Throw(netlink.LinkSetUp(dev))

addr := Throw2(netlink.ParseAddr(networkAddr))
Throw(netlink.AddrAdd(dev, addr))

return dev, addr
```

## Project layout

All `.go` files live in the repo root. No `internal/`, no `cmd/`, no `pkg/`. The project is small; directory hierarchy would be overhead, not structure.

## Config

JSON only. No YAML, ever.

## Dependencies

- S3: `github.com/aws/aws-sdk-go-v2/service/s3`, configured for S3-compatible endpoints (works against MinIO with `UsePathStyle=true`).
- etcd: `go.etcd.io/etcd/client/v3` and `.../concurrency`.
- SSH: **we shell out** to the `ssh` binary via `exec.Command`. A native Go SSH library is tempting, but the separate process gives operational isolation — a stuck connection can be killed on the target host (or locally) without touching the daemon. Classification doesn't need a richer error surface here: the final outcome is read as JSON printed by `gorn wrap` on stdout, not inferred from the ssh exit code.

The shape is "native library when the binary has no clean protocol, shell out when process isolation is worth more than library ergonomics."
