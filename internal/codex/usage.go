package codex

import (
	"context"
	"encoding/json"
	"math"
	"sort"
)

type UsageWindow struct {
	Kind               string   `json:"kind"`
	UsedPercent        *float64 `json:"used_percent"`
	RemainingPercent   *float64 `json:"remaining_percent"`
	WindowDurationMins *int     `json:"window_duration_mins"`
	ResetsAt           *int64   `json:"resets_at"`
}
type UsageLimit struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Windows []UsageWindow `json:"windows"`
}
type Usage struct {
	Available bool         `json:"available"`
	Limits    []UsageLimit `json:"limits"`
}

// Usage reads account rate limits through the official app-server RPC. It does
// not inspect credential files or mutate account state.
func (r *Runtime) Usage(ctx context.Context) (Usage, error) {
	if err := r.acquire(ctx); err != nil {
		return Usage{}, err
	}
	defer r.release()
	c, err := r.start(ctx, 0)
	if err != nil {
		return Usage{}, err
	}
	defer c.close()
	var out struct {
		RateLimits          json.RawMessage            `json:"rateLimits"`
		RateLimitsByLimitID map[string]json.RawMessage `json:"rateLimitsByLimitId"`
	}
	if err := c.call(ctx, "account/rateLimits/read", map[string]any{}, &out); err != nil {
		return Usage{}, err
	}
	return normalizeUsage(out.RateLimits, out.RateLimitsByLimitID), nil
}

type usageLimitRaw struct {
	LimitID   string          `json:"limitId"`
	LimitName string          `json:"limitName"`
	Primary   *usageWindowRaw `json:"primary"`
	Secondary *usageWindowRaw `json:"secondary"`
}
type usageWindowRaw struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int     `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

func normalizeUsage(single json.RawMessage, buckets map[string]json.RawMessage) Usage {
	seen := map[string]bool{}
	result := Usage{Limits: []UsageLimit{}}
	add := func(raw json.RawMessage, fallback string) {
		var v usageLimitRaw
		if json.Unmarshal(raw, &v) != nil {
			return
		}
		if v.LimitID == "" {
			v.LimitID = fallback
		}
		if v.LimitID == "" || seen[v.LimitID] {
			return
		}
		l := UsageLimit{ID: v.LimitID, Name: v.LimitName, Windows: []UsageWindow{}}
		for _, pair := range []struct {
			k string
			w *usageWindowRaw
		}{{"primary", v.Primary}, {"secondary", v.Secondary}} {
			if pair.w != nil {
				l.Windows = append(l.Windows, normalizeUsageWindow(pair.k, pair.w))
			}
		}
		if len(l.Windows) == 0 {
			return
		}
		seen[v.LimitID] = true
		result.Limits = append(result.Limits, l)
	}
	ids := make([]string, 0, len(buckets))
	for id := range buckets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		raw := buckets[id]
		add(raw, id)
	}
	if len(result.Limits) == 0 && len(single) > 0 && string(single) != "null" {
		add(single, "codex")
	}
	for _, limit := range result.Limits {
		for _, window := range limit.Windows {
			if window.UsedPercent != nil {
				result.Available = true
				break
			}
		}
	}
	return result
}
func normalizeUsageWindow(kind string, w *usageWindowRaw) UsageWindow {
	out := UsageWindow{Kind: kind, WindowDurationMins: w.WindowDurationMins, ResetsAt: w.ResetsAt}
	if w.UsedPercent != nil {
		v := math.Max(0, math.Min(100, *w.UsedPercent))
		out.UsedPercent = &v
		remaining := 100 - v
		out.RemainingPercent = &remaining
	}
	return out
}
