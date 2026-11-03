# Standalone Windows Helm validation build

Builds a Windows x64 DLL around the official Helm Go SDK, pinned to Helm v3.22.1. The DLL exposes ABI version 2 for the Python validation adapter.

## Build manually

Open Actions, select **Build native Helm validation library**, choose **Run workflow**, and run the main branch. Pushes and pull requests do not trigger this workflow.

After a successful run, download the **helm-validation-windows-amd64** artifact. It contains the DLL, generated header, manifest, checksums, and a versioned ZIP.

Extract the versioned ZIP, then configure the consuming application with the absolute DAAS_HELM_LIBRARY_PATH and DAAS_HELM_LIBRARY_SHA256 from manifest.json. The application does not need Go, GCC, or the Helm CLI at runtime.

Sources are in Server/native/helm_validation. See docs/helm/NATIVE_HELM_VALIDATION.md for ABI and build details.
