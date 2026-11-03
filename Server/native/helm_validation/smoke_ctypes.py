"""Exercise the compiled C ABI using Python's standard-library ctypes."""

import ctypes
import json
import pathlib
import sys
import tempfile


def call(library, payload):
    pointer = library.DaaSHelmCall(json.dumps(payload).encode("utf-8"))
    if not pointer:
        raise AssertionError("DaaSHelmCall returned a null pointer")
    try:
        length = library.DaaSHelmResponseLength(pointer)
        if length <= 0 or length > 16 * 1024 * 1024:
            raise AssertionError(f"DaaSHelmResponseLength returned invalid size {length}")
        return json.loads(ctypes.string_at(pointer, length).decode("utf-8"))
    finally:
        library.DaaSHelmFree(pointer)


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: smoke_ctypes.py <shared-library>")
    library = ctypes.CDLL(str(pathlib.Path(sys.argv[1]).resolve()))
    library.DaaSHelmCall.argtypes = [ctypes.c_char_p]
    library.DaaSHelmCall.restype = ctypes.c_void_p
    library.DaaSHelmResponseLength.argtypes = [ctypes.c_void_p]
    library.DaaSHelmResponseLength.restype = ctypes.c_size_t
    library.DaaSHelmFree.argtypes = [ctypes.c_void_p]
    library.DaaSHelmFree.restype = None

    version = call(library, {"abi_version": 2, "operation": "version"})
    assert version["ok"] and version["helm_version"] == "v3.22.1"

    with tempfile.TemporaryDirectory(prefix="daas-helm-smoke-") as temporary:
        root = pathlib.Path(temporary).resolve()
        chart = root / "chart"
        (chart / "templates").mkdir(parents=True)
        (chart / "Chart.yaml").write_text(
            "apiVersion: v2\nname: smoke\nversion: 0.1.0\ndescription: ABI smoke test\n",
            encoding="utf-8",
        )
        (chart / "templates" / "config.yaml").write_text(
            "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n",
            encoding="utf-8",
        )
        request = {
            "abi_version": 2,
            "operation": "render",
            "root": str(root),
            "chart_path": str(chart),
            "release_name": "smoke",
            "namespace": "default",
        }
        lint_request = dict(request, operation="lint")
        linted = call(library, lint_request)
        assert linted["ok"] and not linted["errors"]
        rendered = call(library, request)
        assert rendered["ok"] and "kind: ConfigMap" in rendered["manifest"]
        request["chart_path"] = str(root.parent)
        escaped = call(library, request)
        assert not escaped["ok"] and escaped["errors"]


if __name__ == "__main__":
    main()
