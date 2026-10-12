package codex

import (
	"encoding/json"
	"testing"
)

func TestNormalizeUsageMultiBucketNullAndClamp(t *testing.T) {
	buckets := map[string]json.RawMessage{
		"five": json.RawMessage(`{"limitId":"five","limitName":"Five hours","primary":{"usedPercent":120,"windowDurationMins":300,"resetsAt":10},"secondary":null}`),
		"week": json.RawMessage(`{"limitId":"week","primary":{"usedPercent":-2}}`),
	}
	got := normalizeUsage(nil, buckets)
	if !got.Available || len(got.Limits) != 2 {
		t.Fatalf("%#v", got)
	}
	for _, l := range got.Limits {
		if l.ID == "five" {
			if len(l.Windows) != 1 || *l.Windows[0].UsedPercent != 100 || *l.Windows[0].RemainingPercent != 0 {
				t.Fatalf("%#v", l)
			}
		}
		if l.ID == "week" {
			if *l.Windows[0].UsedPercent != 0 || *l.Windows[0].RemainingPercent != 100 {
				t.Fatalf("%#v", l)
			}
		}
	}
}
func TestNormalizeUsageSingleFallbackAndEmpty(t *testing.T) {
	got := normalizeUsage(json.RawMessage(`{"limitId":"one","primary":{"usedPercent":50}}`), nil)
	if !got.Available || len(got.Limits) != 1 || *got.Limits[0].Windows[0].RemainingPercent != 50 {
		t.Fatalf("%#v", got)
	}
	if got := normalizeUsage(nil, nil); got.Available || len(got.Limits) != 0 {
		t.Fatalf("%#v", got)
	}
	if got := normalizeUsage(json.RawMessage(`null`), map[string]json.RawMessage{"nil": json.RawMessage(`null`), "empty": json.RawMessage(`{}`), "unknown": json.RawMessage(`{"limitId":"unknown","primary":{"usedPercent":null}}`)}); got.Available || len(got.Limits) != 1 || got.Limits[0].ID != "unknown" {
		t.Fatalf("%#v", got)
	}
	if got := normalizeUsage(json.RawMessage(`{"primary":{"usedPercent":1}}`), nil); !got.Available || got.Limits[0].ID != "codex" {
		t.Fatalf("%#v", got)
	}
}
