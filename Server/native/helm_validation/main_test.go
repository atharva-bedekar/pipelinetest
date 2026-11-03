package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionOperation(t *testing.T) {
	got := processRequest(`{"abi_version":2,"operation":"version"}`)
	if !got.OK || got.ABIVersion != 2 || got.HelmVersion != helmVersion || len(got.Errors) != 0 {
		t.Fatalf("unexpected version response: %#v", got)
	}
}

func TestRenderRejectsHooksAlongsideValidResources(t *testing.T) {
	root := t.TempDir()
	chartPath := filepath.Join(root, "chart")
	write(t, filepath.Join(chartPath, "Chart.yaml"), "apiVersion: v2\nname: sample\nversion: 0.1.0\n")
	write(t, filepath.Join(chartPath, "templates", "resource.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: valid-resource}\n")
	write(t, filepath.Join(chartPath, "templates", "hook.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: install-hook\n  annotations:\n    helm.sh/hook: pre-install\n")

	got := invoke(t, requestFor(root, chartPath), "render")
	if got.OK || !strings.Contains(strings.Join(got.Errors, " "), "unsupported Helm hooks") {
		t.Fatalf("render accepted a chart containing a hook and valid resource: %#v", got)
	}
}

func TestEncodedResponseStaysWithinBound(t *testing.T) {
	encoded := encodeResponse(response{
		ABIVersion: abiVersion, HelmVersion: helmVersion, Errors: []string{}, Warnings: []string{},
		Manifest: strings.Repeat("x", maxResponseBytes),
	})
	if len(encoded) > maxResponseBytes || !strings.Contains(encoded, "response exceeds the 16 MiB limit") {
		t.Fatalf("response bound was not enforced: %d bytes", len(encoded))
	}
}

func TestLintAndRenderUseOrderedValuesAndExcludeTestTemplates(t *testing.T) {
	root := t.TempDir()
	chartPath := filepath.Join(root, "chart")
	write(t, filepath.Join(chartPath, "Chart.yaml"), "apiVersion: v2\nname: sample\nversion: 0.1.0\ndescription: sample chart\n")
	write(t, filepath.Join(chartPath, "values.yaml"), "image: default:1\n")
	write(t, filepath.Join(chartPath, "templates", "deployment.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n  namespace: {{ .Release.Namespace }}\ndata:\n  image: {{ .Values.image | quote }}\n")
	write(t, filepath.Join(chartPath, "templates", "tests", "probe.yaml"), "apiVersion: v1\nkind: Secret\nmetadata:\n  name: should-not-render\n")
	first := filepath.Join(root, "first.yaml")
	second := filepath.Join(root, "second.yaml")
	write(t, first, "image: first:1\n")
	write(t, second, "image: second:2\n")
	request := requestFor(root, chartPath)
	request.ValuesFiles = []string{first, second}

	lint := invoke(t, request, "lint")
	if !lint.OK {
		t.Fatalf("valid Helm chart failed lint: %#v", lint)
	}
	rendered := invoke(t, request, "render")
	if !rendered.OK || !strings.Contains(rendered.Manifest, `image: "second:2"`) || !strings.Contains(rendered.Manifest, "namespace: default") {
		t.Fatalf("render did not use effective values and namespace: %#v", rendered)
	}
	if strings.Contains(rendered.Manifest, "should-not-render") {
		t.Fatal("test template was rendered")
	}
}

func TestRejectsEscapingPathsMalformedValuesAndSchemaMismatch(t *testing.T) {
	root := t.TempDir()
	chartPath := filepath.Join(root, "chart")
	write(t, filepath.Join(chartPath, "Chart.yaml"), "apiVersion: v2\nname: sample\nversion: 0.1.0\n")
	write(t, filepath.Join(chartPath, "values.yaml"), "image: default:1\n")
	write(t, filepath.Join(chartPath, "templates", "config.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample\n")
	request := requestFor(root, chartPath)

	request.ChartPath = filepath.Dir(root)
	if got := invoke(t, request, "lint"); got.OK || len(got.Errors) == 0 {
		t.Fatalf("accepted chart path outside root: %#v", got)
	}
	request = requestFor(root, chartPath)
	malformed := filepath.Join(root, "bad.yaml")
	write(t, malformed, "image: [unterminated\n")
	request.ValuesFiles = []string{malformed}
	if got := invoke(t, request, "lint"); got.OK || !strings.Contains(strings.Join(got.Errors, " "), "invalid YAML") {
		t.Fatalf("accepted malformed values: %#v", got)
	}
	request = requestFor(root, chartPath)
	write(t, filepath.Join(chartPath, "values.schema.json"), `{"type":"object","properties":{"image":{"type":"string"}},"required":["image"]}`)
	request.Overrides = map[string]interface{}{"image": 42}
	if got := invoke(t, request, "render"); got.OK || len(got.Errors) == 0 {
		t.Fatalf("accepted values that violate the chart schema: %#v", got)
	}
}

func TestRejectsBadRequestsAndBounds(t *testing.T) {
	for _, raw := range []string{"{", `{"abi_version":1,"operation":"version"}`, strings.Repeat("x", maxRequestBytes+1)} {
		if got := processRequest(raw); got.OK || len(got.Errors) == 0 {
			t.Fatalf("accepted invalid request (%d bytes): %#v", len(raw), got)
		}
	}
}

func requestFor(root, chartPath string) request {
	return request{ABIVersion: 2, Operation: "lint", Root: root, ChartPath: chartPath, ReleaseName: "sample", Namespace: "default"}
}

func invoke(t *testing.T, req request, operation string) response {
	t.Helper()
	req.Operation = operation
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return processRequest(string(encoded))
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
