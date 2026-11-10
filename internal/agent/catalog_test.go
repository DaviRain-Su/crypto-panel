package agent

import (
	"testing"

	"github.com/davirain-su/lintel/internal/pages"
)

func TestCatalogDescribesChainCapabilitiesAndAuthoringExamples(t *testing.T) {
	catalog := Catalog(true)
	functions := catalog["hostFunctions"].([]map[string]any)
	want := map[string]string{
		"host.units.parse":      "none",
		"host.units.format":     "none",
		"host.hyperliquid.info": "network: api.hyperliquid.xyz or api.hyperliquid-testnet.xyz",
		"host.near.view":        "NEAR chain",
		"host.intents.tokens":   "network: 1click.chaindefuser.com",
		"host.intents.dryQuote": "network: 1click.chaindefuser.com; dry only",
		"host.intents.status":   "network: 1click.chaindefuser.com",
	}
	for name, permission := range want {
		found := false
		for _, fn := range functions {
			if fn["name"] == name {
				found = true
				if fn["permission"] != permission || fn["inputSchema"] == nil || fn["outputSchema"] == nil {
					t.Fatalf("%s = %+v", name, fn)
				}
			}
		}
		if !found {
			t.Errorf("catalog does not describe %s", name)
		}
	}
	for _, fn := range functions {
		if fn["name"] != "host.hyperliquid.info" {
			continue
		}
		input := fn["inputSchema"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)
		properties := input["properties"].(map[string]any)
		for _, field := range []string{"coin", "nSigFigs", "mantissa", "req", "user", "dex", "startTime", "endTime", "aggregateByTime"} {
			if properties[field] == nil {
				t.Errorf("Hyperliquid catalog omits operation parameter %q", field)
			}
		}
	}
	for _, fn := range functions {
		name := fn["name"]
		if name != "host.intents.dryQuote" && name != "host.intents.status" {
			continue
		}
		output := fn["outputSchema"].(map[string]any)
		if output["additionalProperties"] != true {
			t.Errorf("%s output rejects trusted protocol fields", name)
		}
		properties := output["properties"].(map[string]any)
		if name == "host.intents.status" && (properties["quoteResponse"] == nil || properties["swapDetails"] == nil) {
			t.Errorf("status output omits raw protocol fields: %#v", properties)
		}
	}
	examples := catalog["examples"].([]map[string]any)
	for _, id := range []string{"com.example.hyperliquid", "com.example.intents"} {
		found := false
		for _, example := range examples {
			if example["id"] == id {
				found = true
			}
		}
		if !found {
			t.Errorf("catalog does not package %s", id)
		}
	}
}

func TestCatalogDescribesTrendSparklineGaugeAndLimits(t *testing.T) {
	catalog := Catalog(true)
	schemas := catalog["schemas"].(map[string]any)
	page := schemas["page"].(map[string]any)
	pageProperties := page["properties"].(map[string]any)
	sections := pageProperties["sections"].(map[string]any)
	section := sections["items"].(map[string]any)
	properties := section["properties"].(map[string]any)
	stats := properties["stats"].(map[string]any)
	stat := stats["items"].(map[string]any)
	statProperties := stat["properties"].(map[string]any)
	trend := statProperties["trend"].(map[string]any)
	values := trend["enum"].([]string)
	if len(values) != 3 || values[0] != "" || values[1] != "up" || values[2] != "down" {
		t.Fatalf("trend schema = %#v", trend)
	}
	sparkline := statProperties["sparkline"].(map[string]any)
	if sparkline["maxItems"] != 32 || sparkline["items"].(map[string]any)["type"] != "number" {
		t.Fatalf("sparkline schema = %#v", sparkline)
	}
	gauge := properties["gauge"].(map[string]any)
	gaugeProperties := gauge["properties"].(map[string]any)
	value := gaugeProperties["value"].(map[string]any)
	if value["minimum"] != float64(0) || value["maximum"] != float64(100) {
		t.Fatalf("gauge value schema = %#v", value)
	}
	limits := catalog["limits"].(map[string]int)
	if limits["sparklinePoints"] != 32 || limits["gaugeMin"] != 0 || limits["gaugeMax"] != 100 {
		t.Fatalf("limits = %#v", limits)
	}
	valid := pages.Page{Sections: []pages.Section{{
		Stats: []pages.Stat{{Label: "Price", Value: "42", Trend: "up", Sparkline: []float64{1, 2, 3}}},
		Gauge: &pages.Gauge{Label: "Health", Value: 100, Text: "100%"},
	}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("the runtime rejects the catalog's valid page: %v", err)
	}
	for name, page := range map[string]pages.Page{
		"trend":     {Sections: []pages.Section{{Stats: []pages.Stat{{Label: "a", Value: "1", Trend: "sideways"}}}}},
		"sparkline": {Sections: []pages.Section{{Stats: []pages.Stat{{Label: "a", Value: "1", Sparkline: make([]float64, 33)}}}}},
		"gauge":     {Sections: []pages.Section{{Gauge: &pages.Gauge{Value: 101}}}},
	} {
		if err := page.Validate(); err == nil {
			t.Errorf("invalid %s was accepted", name)
		}
	}
}
