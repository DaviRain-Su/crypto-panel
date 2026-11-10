package agent

import (
	"testing"

	"github.com/davirain-su/lintel/internal/pages"
)

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
