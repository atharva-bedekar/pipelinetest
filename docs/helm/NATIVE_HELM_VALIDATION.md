# Native Helm validation library

The Windows server can call the official Helm Go SDK through a prebuilt shared library. Runtime Python uses the standard-library `ctypes` loader; it does not need Go, a Helm executable, network access, or a Kubernetes cluster. The SDK is pinned to `helm.sh/helm/v3 v3.22.1` and Go 1.26.0.

## Request and response

The C ABI version 2 exports `DaaSHelmCall(const char *request_json) -> char *`, `DaaSHelmResponseLength(void *response) -> size_t`, and `DaaSHelmFree(void *response)`. The length export returns the exact UTF-8 byte length without scanning the allocation. The returned buffer belongs to the library and must be released with `DaaSHelmFree` exactly once, including when the length or JSON is rejected. Request and response objects use `abi_version: 2`; responses always include `abi_version`, `ok`, `helm_version`, `errors`, `warnings`, and `manifest`. Responses above 16 MiB are replaced with a bounded error response.

Every request includes `abi_version: 2` and `operation`. `version` returns the SDK version. `lint` and `render` also take absolute `root` and `chart_path` paths, plus `release_name` and `namespace`. `values_files` is an optional ordered array of absolute paths under `root`; later files override earlier ones. `overrides` is an optional nested JSON object applied last. `kube_version` is optional. The chart and values paths are resolved through symlinks and must remain inside `root`.

Lint uses Helm's SDK lint action in strict mode, including chart schema and template checks. Render uses Helm's SDK install action in client-only dry-run mode, includes CRDs, excludes NOTES and test templates, and returns the resulting manifest. Neither operation contacts a cluster, resolves or downloads dependencies, or invokes shell commands. Requests, chart trees, and values files have size/count limits.

## Build and verify

The manual GitHub Actions workflow builds and runs focused Go tests on Windows amd64, then smoke-tests the shared library using Python `ctypes`. It packages the library, generated C header, ABI/platform/SHA256 manifest, and checksum list as a downloadable workflow artifact. Artifacts expire after 30 days. To retain shareable versioned binaries, manually dispatch the workflow with `publish_release=true` and a tag such as `helm-native-v1.0.0`; this explicitly creates a GitHub Release with the Windows bundle. There are no push or pull request triggers.

Local maintainers can run `Server/native/helm_validation/build.ps1 [-OutDir output-directory]` on Windows with Go 1.26.0, Python 3, and a MinGW UCRT64 GCC toolchain installed. Generated artifacts go to the selected output directory and are not checked into the repository.

Rebuild and publish when the wrapper or pinned Helm SDK changes, when refreshing platform toolchains/security updates, or when introducing a new supported platform. Ordinary application pushes do not rebuild or republish the native dependency.

The Python adapter reads one explicit library location from `DAAS_HELM_LIBRARY_PATH` and verifies it against `DAAS_HELM_LIBRARY_SHA256`. It does not search for arbitrary library candidates.
