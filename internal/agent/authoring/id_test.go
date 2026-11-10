package authoring

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/davirain-su/lintel/internal/plugin"
)

func authoringRequest() Request {
	return Request{
		Prompt:  "build a portfolio",
		Catalog: json.RawMessage(`{"plugins":[]}`),
		Examples: []plugin.Package{{
			Plugin: json.RawMessage(`{"id":"local.personal.example"}`),
			Main:   "export function page(){return {sections:[]};}",
		}},
	}
}

func TestAuthoringRejectsFourSegmentPersonalID(t *testing.T) {
	create := authoringRequest()
	create.TargetID = "local.personal.ok"
	if err := validateRequest(create); err != nil {
		t.Fatal(err)
	}
	create.TargetID = "local.personal.a.b"
	if err := validateRequest(create); err == nil {
		t.Fatal("create accepted a four-segment id")
	}

	update := authoringRequest()
	update.ExistingDir = t.TempDir()
	update.ExpectedHash = "sha256:" + strings.Repeat("ab", 32)
	update.TargetID = "local.personal.ok"
	if err := validateRequest(update); err != nil {
		t.Fatal(err)
	}
	update.TargetID = "local.personal.a.b"
	if err := validateRequest(update); err == nil {
		t.Fatal("update accepted a four-segment id")
	}
}
