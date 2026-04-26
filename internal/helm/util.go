package helm

import (
	"time"

	"github.com/aerol-ai/kubeshipper/internal/store"

	"sigs.k8s.io/yaml"
)

func emit(emit EmitFn, phase, msg string) {
	if emit == nil {
		return
	}
	emit(store.Event{Phase: phase, Message: msg, TS: time.Now().UnixMilli()})
}

func emitErr(emit EmitFn, msg string) {
	if emit == nil {
		return
	}
	emit(store.Event{Phase: "error", Error: msg, TS: time.Now().UnixMilli()})
}

func valuesToYAML(v map[string]any) (string, error) {
	if len(v) == 0 {
		return "", nil
	}
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseValuesYAML(s string) (map[string]any, error) {
	if s == "" {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := yaml.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func timeoutOrDefault(seconds int, dflt time.Duration) time.Duration {
	if seconds <= 0 {
		return dflt
	}
	return time.Duration(seconds) * time.Second
}

func boolDefault(p *bool, dflt bool) bool {
	if p == nil {
		return dflt
	}
	return *p
}
