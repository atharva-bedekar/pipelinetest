package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/lint/support"
)

const (
	abiVersion       = 2
	helmVersion      = "v3.22.1"
	maxRequestBytes  = 1 << 20
	maxResponseBytes = 16 << 20
	maxValuesBytes   = 4 << 20
	maxChartBytes    = 64 << 20
	maxChartFiles    = 4096
)

type request struct {
	ABIVersion  int                    `json:"abi_version"`
	Operation   string                 `json:"operation"`
	Root        string                 `json:"root"`
	ChartPath   string                 `json:"chart_path"`
	ReleaseName string                 `json:"release_name"`
	Namespace   string                 `json:"namespace"`
	ValuesFiles []string               `json:"values_files"`
	Overrides   map[string]interface{} `json:"overrides"`
	KubeVersion string                 `json:"kube_version"`
}

type response struct {
	ABIVersion  int      `json:"abi_version"`
	OK          bool     `json:"ok"`
	HelmVersion string   `json:"helm_version"`
	Errors      []string `json:"errors"`
	Warnings    []string `json:"warnings"`
	Manifest    string   `json:"manifest"`
}

var responseLengths sync.Map

//export DaaSHelmCall
func DaaSHelmCall(input *C.char) (output *C.char) {
	defer func() {
		if recover() != nil {
			output = newResponseBuffer(failure("Helm validation failed unexpectedly"))
		}
	}()
	if input == nil {
		return newResponseBuffer(failure("request is required"))
	}
	return newResponseBuffer(processRequest(C.GoString(input)))
}

//export DaaSHelmResponseLength
func DaaSHelmResponseLength(pointer *C.char) C.size_t {
	if pointer == nil {
		return 0
	}
	if length, ok := responseLengths.Load(uintptr(unsafe.Pointer(pointer))); ok {
		return length.(C.size_t)
	}
	return 0
}

//export DaaSHelmFree
func DaaSHelmFree(pointer unsafe.Pointer) {
	if pointer != nil {
		responseLengths.Delete(uintptr(pointer))
	}
	C.free(pointer)
}

func main() {}

func failure(message string) response {
	return response{ABIVersion: abiVersion, HelmVersion: helmVersion, Errors: []string{message}}
}

func encodeResponse(value response) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `{"abi_version":2,"ok":false,"helm_version":"v3.22.1","errors":["response encoding failed"],"warnings":[],"manifest":""}`
	}
	if len(encoded) > maxResponseBytes {
		encoded, _ = json.Marshal(failure("Helm response exceeds the 16 MiB limit"))
	}
	return string(encoded)
}

func newResponseBuffer(value response) *C.char {
	encoded := encodeResponse(value)
	buffer := C.CString(encoded)
	responseLengths.Store(uintptr(unsafe.Pointer(buffer)), C.size_t(len(encoded)))
	return buffer
}

func processRequest(raw string) response {
	if len(raw) > maxRequestBytes {
		return failure("request exceeds the 1 MiB limit")
	}
	var req request
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&req); err != nil {
		return failure("request must be valid JSON")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return failure("request must contain one JSON object")
	}
	if req.ABIVersion != abiVersion {
		return failure("unsupported ABI version")
	}
	if req.Operation == "version" {
		return response{ABIVersion: abiVersion, OK: true, HelmVersion: helmVersion, Errors: []string{}, Warnings: []string{}, Manifest: ""}
	}
	if req.Operation != "lint" && req.Operation != "render" {
		return failure("unsupported operation")
	}
	chartPath, valuesFiles, err := validatePaths(req)
	if err != nil {
		return failure(err.Error())
	}
	if strings.TrimSpace(req.ReleaseName) == "" || strings.TrimSpace(req.Namespace) == "" {
		return failure("release_name and namespace are required")
	}
	values, err := effectiveValues(valuesFiles, req.Overrides)
	if err != nil {
		return failure(err.Error())
	}
	if err := checkChartSize(chartPath); err != nil {
		return failure(err.Error())
	}
	result := response{ABIVersion: abiVersion, HelmVersion: helmVersion, Errors: []string{}, Warnings: []string{}, Manifest: ""}
	switch req.Operation {
	case "lint":
		lint := action.NewLint()
		lint.Strict = true
		lint.Namespace = req.Namespace
		if req.KubeVersion != "" {
			version, parseErr := chartutil.ParseKubeVersion(req.KubeVersion)
			if parseErr != nil {
				return failure("kube_version is invalid")
			}
			lint.KubeVersion = version
		}
		lintResult := lint.Run([]string{chartPath}, values)
		for _, message := range lintResult.Messages {
			text := safeDiagnostic(message.Error())
			switch message.Severity {
			case support.ErrorSev:
				result.Errors = append(result.Errors, text)
			case support.WarningSev:
				result.Warnings = append(result.Warnings, text)
			}
		}
		result.OK = len(result.Errors) == 0 && len(result.Warnings) == 0
	case "render":
		chrt, loadErr := loader.Load(chartPath)
		if loadErr != nil {
			return failure(safeDiagnostic(loadErr.Error()))
		}
		removeTestTemplates(chrt)
		install := action.NewInstall(&action.Configuration{})
		install.ClientOnly = true
		install.DryRun = true
		install.IncludeCRDs = true
		install.ReleaseName = req.ReleaseName
		install.Namespace = req.Namespace
		if req.KubeVersion != "" {
			version, parseErr := chartutil.ParseKubeVersion(req.KubeVersion)
			if parseErr != nil {
				return failure("kube_version is invalid")
			}
			install.KubeVersion = version
		}
		release, runErr := install.Run(chrt, values)
		if runErr != nil {
			return failure(safeDiagnostic(runErr.Error()))
		}
		if len(release.Hooks) > 0 {
			return failure("chart uses unsupported Helm hooks")
		}
		result.Manifest = release.Manifest
		result.OK = strings.TrimSpace(result.Manifest) != ""
		if !result.OK {
			result.Errors = append(result.Errors, "chart rendered no Kubernetes resources")
		}
	}
	return result
}

func validatePaths(req request) (string, []string, error) {
	if req.Root == "" || !filepath.IsAbs(req.Root) || req.ChartPath == "" || !filepath.IsAbs(req.ChartPath) {
		return "", nil, errors.New("root and chart_path must be absolute paths")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(req.Root))
	if err != nil {
		return "", nil, errors.New("root does not exist")
	}
	chartPath, err := containedPath(root, req.ChartPath)
	if err != nil {
		return "", nil, errors.New("chart_path must resolve inside root")
	}
	info, err := os.Stat(chartPath)
	if err != nil || !info.IsDir() {
		return "", nil, errors.New("chart_path must be an existing chart directory")
	}
	if _, err := os.Stat(filepath.Join(chartPath, "Chart.yaml")); err != nil {
		return "", nil, errors.New("chart_path does not contain Chart.yaml")
	}
	if len(req.ValuesFiles) > 32 {
		return "", nil, errors.New("values_files exceeds the 32-file limit")
	}
	values := make([]string, 0, len(req.ValuesFiles))
	for _, path := range req.ValuesFiles {
		if !filepath.IsAbs(path) {
			return "", nil, errors.New("values_files entries must be absolute paths")
		}
		resolved, pathErr := containedPath(root, path)
		if pathErr != nil {
			return "", nil, errors.New("values_files entries must resolve inside root")
		}
		fileInfo, statErr := os.Stat(resolved)
		if statErr != nil || !fileInfo.Mode().IsRegular() {
			return "", nil, errors.New("values_files entries must be existing regular files")
		}
		if fileInfo.Size() > maxValuesBytes {
			return "", nil, errors.New("a values file exceeds the 4 MiB limit")
		}
		values = append(values, resolved)
	}
	return chartPath, values, nil
}

func containedPath(root, candidate string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(candidate))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("path escapes root")
	}
	return resolved, nil
}

func effectiveValues(files []string, overrides map[string]interface{}) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("unable to read values file")
		}
		parsed, err := chartutil.ReadValues(data)
		if err != nil {
			return nil, errors.New("values file contains invalid YAML")
		}
		mergeMaps(values, parsed)
	}
	mergeMaps(values, overrides)
	return values, nil
}

func mergeMaps(target, source map[string]interface{}) {
	for key, value := range source {
		if sourceMap, ok := value.(map[string]interface{}); ok {
			if targetMap, exists := target[key].(map[string]interface{}); exists {
				mergeMaps(targetMap, sourceMap)
				continue
			}
		}
		target[key] = value
	}
}

func checkChartSize(root string) error {
	files, total := 0, int64(0)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("chart contains a symbolic link")
		}
		if info.IsDir() {
			return nil
		}
		files++
		total += info.Size()
		if files > maxChartFiles || total > maxChartBytes {
			return errors.New("chart exceeds file or size limits")
		}
		return nil
	})
	if err != nil {
		return errors.New("unable to read chart within safety limits")
	}
	return nil
}

func removeTestTemplates(chrt *chart.Chart) {
	kept := chrt.Templates[:0]
	for _, template := range chrt.Templates {
		name := strings.ToLower(filepath.ToSlash(template.Name))
		base := filepath.Base(name)
		if strings.HasSuffix(name, "/tests/") || strings.Contains(name, "/tests/") || strings.HasSuffix(base, "_test.yaml") || strings.HasSuffix(base, "_test.yml") {
			continue
		}
		kept = append(kept, template)
	}
	chrt.Templates = kept
	for _, dependency := range chrt.Dependencies() {
		removeTestTemplates(dependency)
	}
}

func safeDiagnostic(message string) string {
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	for _, secret := range []string{"password", "secret", "token", "credential", "api_key"} {
		lower := strings.ToLower(message)
		if index := strings.Index(lower, secret); index >= 0 {
			message = message[:index] + secret + "=[REDACTED]"
		}
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	return message
}
