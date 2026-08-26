//go:build windows

package powershell

import (
	"fmt"
	"os"
	"strings"
)

// extractRuntimeInject reads policy-injected env and temp file paths from tool args.
func extractRuntimeInject(args map[string]any) (env map[string]string, tempFiles []string) {
	if args == nil {
		return nil, nil
	}
	if raw, ok := args["_runtime_env"].(map[string]string); ok && len(raw) > 0 {
		env = make(map[string]string, len(raw))
		for k, v := range raw {
			env[k] = v
		}
	} else if rawAny, ok := args["_runtime_env"].(map[string]any); ok && len(rawAny) > 0 {
		env = make(map[string]string, len(rawAny))
		for k, v := range rawAny {
			env[k] = fmt.Sprint(v)
		}
	}
	switch t := args["_runtime_inject_tempfiles"].(type) {
	case []string:
		tempFiles = append(tempFiles, t...)
	case []any:
		for _, x := range t {
			tempFiles = append(tempFiles, fmt.Sprint(x))
		}
	}
	return env, tempFiles
}

func mergeOSEnv(extra map[string]string) []string {
	base := os.Environ()
	if len(extra) == 0 {
		return base
	}
	m := environToMap(base)
	for k, v := range extra {
		m[k] = v
	}
	return mapToEnviron(m)
}

func environToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			m[k] = v
		} else {
			m[e] = ""
		}
	}
	return m
}

func mapToEnviron(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// maskSecrets replaces injected credential values in the output with ***.
func maskSecrets(output string, env map[string]string) string {
	for _, v := range env {
		if v == "" {
			continue
		}
		output = strings.ReplaceAll(output, v, "***")
	}
	return output
}

func cleanupTempPaths(paths []string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		_ = os.Remove(p)
	}
}
