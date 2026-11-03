# Native Helm validation

This module builds the precompiled C shared library used by the Python `ctypes` adapter. It wraps official `helm.sh/helm/v3` SDK v3.22.1; runtime hosts need only the matching OS/architecture library and Python's standard library.

The ABI is documented in [`NATIVE_HELM_VALIDATION.md`](../../../docs/helm/NATIVE_HELM_VALIDATION.md). ABI version 2 exports `DaaSHelmCall(const char *)`, `DaaSHelmResponseLength(void *)`, and `DaaSHelmFree(void *)`. Callers must check the exact byte length before reading and free every returned buffer exactly once.

Run `go test ./...` for wrapper tests. Use `./build.ps1 [-OutDir output-directory]` on Windows to run tests, build the Windows amd64 c-shared library, execute the `ctypes` smoke test, and emit a versioned archive, generated C header, manifest, and SHA256 file. Go 1.26.0 and MinGW UCRT64 GCC are required only for building.

The library does not run `helm`, shell commands, network access, or cluster calls. It bounds requests and chart inputs, validates paths against the request root, and reports all request-level failures through the JSON response.
