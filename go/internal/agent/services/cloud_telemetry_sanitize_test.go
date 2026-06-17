package services

import (
	"strings"
	"testing"

	otelpb "github.com/wendylabsinc/wendy/go/proto/gen/otelpb"
)

func kv(key, val string) *otelpb.KeyValue {
	return &otelpb.KeyValue{
		Key:   key,
		Value: &otelpb.AnyValue{Value: &otelpb.AnyValue_StringValue{StringValue: val}},
	}
}

func TestSanitizeAttributes_DropsSensitiveAndTruncates(t *testing.T) {
	in := []*otelpb.KeyValue{
		kv("user", "alice"),
		kv("password", "hunter2"),       // dropped by deny-list
		kv("auth_header", "Bearer xyz"), // dropped (contains "auth")
		kv(strings.Repeat("k", maxLabelKeyLen+10), "v"),
		kv("big", strings.Repeat("v", maxLabelValLen+10)),
	}
	out := sanitizeAttributes(in)

	for _, a := range out {
		if isSensitiveLabelKey(a.GetKey()) {
			t.Errorf("sensitive key survived: %q", a.GetKey())
		}
		if len(a.GetKey()) > maxLabelKeyLen {
			t.Errorf("key not truncated: len=%d", len(a.GetKey()))
		}
		if len(a.GetValue().GetStringValue()) > maxLabelValLen {
			t.Errorf("value not truncated: len=%d", len(a.GetValue().GetStringValue()))
		}
	}
	if len(out) != 3 { // user, truncated-key, big
		t.Fatalf("want 3 surviving attrs, got %d", len(out))
	}
}

func TestSanitizeAttributes_CapsCount(t *testing.T) {
	in := make([]*otelpb.KeyValue, 0, maxLabels+10)
	for i := 0; i < maxLabels+10; i++ {
		in = append(in, kv("k", "v"))
	}
	if got := len(sanitizeAttributes(in)); got != maxLabels {
		t.Fatalf("want %d (capped), got %d", maxLabels, got)
	}
}

func TestSanitizeLogs_TruncatesBodyAndScrubsAttrs(t *testing.T) {
	req := &otelpb.ExportLogsServiceRequest{
		ResourceLogs: []*otelpb.ResourceLogs{{
			Resource: &otelpb.Resource{Attributes: []*otelpb.KeyValue{kv("token", "abc")}},
			ScopeLogs: []*otelpb.ScopeLogs{{
				LogRecords: []*otelpb.LogRecord{{
					Body:       &otelpb.AnyValue{Value: &otelpb.AnyValue_StringValue{StringValue: strings.Repeat("x", maxLogBodyBytes+100)}},
					Attributes: []*otelpb.KeyValue{kv("secret", "s"), kv("ok", "1")},
				}},
			}},
		}},
	}
	sanitizeLogs(req)

	rl := req.GetResourceLogs()[0]
	if len(rl.GetResource().GetAttributes()) != 0 {
		t.Errorf("resource token attr should be dropped")
	}
	lr := rl.GetScopeLogs()[0].GetLogRecords()[0]
	if len(lr.GetBody().GetStringValue()) != maxLogBodyBytes {
		t.Errorf("body not truncated to %d, got %d", maxLogBodyBytes, len(lr.GetBody().GetStringValue()))
	}
	if len(lr.GetAttributes()) != 1 || lr.GetAttributes()[0].GetKey() != "ok" {
		t.Errorf("record attrs not scrubbed correctly: %+v", lr.GetAttributes())
	}
}

func TestSanitizeMetrics_ScrubsDataPointAttrs(t *testing.T) {
	req := &otelpb.ExportMetricsServiceRequest{
		ResourceMetrics: []*otelpb.ResourceMetrics{{
			ScopeMetrics: []*otelpb.ScopeMetrics{{
				Metrics: []*otelpb.Metric{{
					Name: "m",
					Data: &otelpb.Metric_Gauge{Gauge: &otelpb.Gauge{
						DataPoints: []*otelpb.NumberDataPoint{{
							Attributes: []*otelpb.KeyValue{kv("api_key", "k"), kv("region", "us")},
						}},
					}},
				}},
			}},
		}},
	}
	sanitizeMetrics(req)

	dp := req.GetResourceMetrics()[0].GetScopeMetrics()[0].GetMetrics()[0].GetGauge().GetDataPoints()[0]
	if len(dp.GetAttributes()) != 1 || dp.GetAttributes()[0].GetKey() != "region" {
		t.Errorf("datapoint attrs not scrubbed: %+v", dp.GetAttributes())
	}
}

func TestSanitizeTraces_ScrubsSpanAttrs(t *testing.T) {
	req := &otelpb.ExportTraceServiceRequest{
		ResourceSpans: []*otelpb.ResourceSpans{{
			ScopeSpans: []*otelpb.ScopeSpans{{
				Spans: []*otelpb.Span{{
					Name:       "s",
					Attributes: []*otelpb.KeyValue{kv("credential", "c"), kv("route", "/x")},
				}},
			}},
		}},
	}
	sanitizeTraces(req)

	sp := req.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0]
	if len(sp.GetAttributes()) != 1 || sp.GetAttributes()[0].GetKey() != "route" {
		t.Errorf("span attrs not scrubbed: %+v", sp.GetAttributes())
	}
}
