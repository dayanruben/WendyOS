package appconfig

import (
	"encoding/json"
	"testing"
)

// cameraFleetJSON mirrors the camera-fleet template's wendy-fleet.json
// (templates PR #59) with template variables rendered to concrete values. It is
// the canonical valid fleet manifest and acts as the acceptance fixture for the
// fleet placement schema (WDY-1755).
const cameraFleetJSON = `{
  "appId": "sh.wendy.examples.camerafleet",
  "version": "0.1.0",
  "platform": "linux/arm64",
  "components": {
    "camera": {
      "context": "camera",
      "target": { "group": "camera-*" },
      "expose": { "name": "usbcam", "port": 8000, "path": "/stream" },
      "entitlements": [
        { "type": "network", "mode": "host" },
        { "type": "camera" }
      ],
      "readiness": { "tcpSocket": { "port": 8000 }, "timeoutSeconds": 30 }
    },
    "dashboard": {
      "context": "dashboard",
      "target": { "central": true },
      "discovers": [ { "component": "camera", "as": "WENDY_FLEET_PEERS" } ],
      "entitlements": [ { "type": "network", "mode": "host" } ],
      "readiness": { "tcpSocket": { "port": 9000 }, "timeoutSeconds": 30 },
      "hooks": { "postStart": { "openURL": "http://${WENDY_HOSTNAME}:9000" } }
    }
  }
}`

func TestFleetManifest_Valid(t *testing.T) {
	m, err := ParseFleetManifest([]byte(cameraFleetJSON))
	if err != nil {
		t.Fatalf("ParseFleetManifest: %v", err)
	}
	if m.AppID != "sh.wendy.examples.camerafleet" {
		t.Errorf("appId = %q", m.AppID)
	}
	if len(m.Components) != 2 {
		t.Fatalf("got %d components, want 2", len(m.Components))
	}
	cam := m.Components["camera"]
	if cam == nil || cam.Target == nil || cam.Target.Group != "camera-*" {
		t.Fatalf("camera component target group not parsed: %+v", cam)
	}
	if cam.Expose == nil || cam.Expose.Port != 8000 || cam.Expose.Path != "/stream" {
		t.Fatalf("camera expose not parsed: %+v", cam.Expose)
	}
	dash := m.Components["dashboard"]
	if dash == nil || dash.Target == nil || !dash.Target.Central {
		t.Fatalf("dashboard component target central not parsed: %+v", dash)
	}
	if len(dash.Discovers) != 1 || dash.Discovers[0].Component != "camera" || dash.Discovers[0].As != "WENDY_FLEET_PEERS" {
		t.Fatalf("dashboard discovers not parsed: %+v", dash.Discovers)
	}
}

func TestFleetManifest_RequiresAppIDAndComponents(t *testing.T) {
	if _, err := ParseFleetManifest([]byte(`{"components":{"c":{"context":"c","target":{"central":true}}}}`)); err == nil {
		t.Error("expected error for missing appId")
	}
	if _, err := ParseFleetManifest([]byte(`{"appId":"sh.wendy.app","components":{}}`)); err == nil {
		t.Error("expected error for empty components")
	}
}

func TestComponentTarget_Validation(t *testing.T) {
	tests := []struct {
		name   string
		target string
		ok     bool
	}{
		{"group only", `{ "group": "cameras" }`, true},
		{"central only", `{ "central": true }`, true},
		{"both set", `{ "group": "cameras", "central": true }`, false},
		{"neither set", `{}`, false},
		{"missing target", ``, false},
		{"empty group", `{ "group": "" }`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := `{"context":"c"`
			if tt.target != "" {
				comp += `,"target":` + tt.target
			}
			comp += `}`
			raw := `{"appId":"sh.wendy.app","components":{"c":` + comp + `}}`
			_, err := ParseFleetManifest([]byte(raw))
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestComponentExpose_Validation(t *testing.T) {
	tests := []struct {
		name   string
		expose string
		ok     bool
	}{
		{"valid", `{ "name": "usbcam", "port": 8000, "path": "/stream" }`, true},
		{"port zero", `{ "port": 0 }`, false},
		{"port too high", `{ "port": 70000 }`, false},
		{"path no slash", `{ "port": 8000, "path": "stream" }`, false},
		{"bad name", `{ "port": 8000, "name": "Bad_Name" }`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"appId":"sh.wendy.app","components":{"c":{"context":"c","target":{"group":"g"},"expose":` + tt.expose + `}}}`
			_, err := ParseFleetManifest([]byte(raw))
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDiscovers_Validation(t *testing.T) {
	// base has an edge "cam" (group) and a central "c" we attach discovers to.
	build := func(discovers string) string {
		return `{"appId":"sh.wendy.app","components":{` +
			`"cam":{"context":"cam","target":{"group":"cams"}},` +
			`"c":{"context":"c","target":{"central":true},"discovers":` + discovers + `}}}`
	}
	tests := []struct {
		name      string
		discovers string
		ok        bool
	}{
		{"valid", `[{ "component": "cam", "as": "WENDY_FLEET_PEERS" }]`, true},
		{"unknown component", `[{ "component": "nope", "as": "PEERS" }]`, false},
		{"reserved env var", `[{ "component": "cam", "as": "WENDY_DISCOVERY_URL" }]`, false},
		{"bad env var name", `[{ "component": "cam", "as": "1bad" }]`, false},
		{"missing as", `[{ "component": "cam" }]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFleetManifest([]byte(build(tt.discovers)))
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDiscovers_RejectsDiscoveringCentral(t *testing.T) {
	// Discovering a central (non-group) component is meaningless and rejected.
	raw := `{"appId":"sh.wendy.app","components":{` +
		`"a":{"context":"a","target":{"central":true}},` +
		`"b":{"context":"b","target":{"central":true},"discovers":[{"component":"a","as":"PEERS"}]}}}`
	if _, err := ParseFleetManifest([]byte(raw)); err == nil {
		t.Fatal("expected error discovering a non-group component, got nil")
	}
}

func TestFleetSchemaJSON_HasComponents(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(FleetSchemaJSON), &schema); err != nil {
		t.Fatalf("fleet schema is not valid JSON: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["components"]; !ok {
		t.Error("fleet schema missing top-level 'components' property")
	}
	defs, _ := schema["$defs"].(map[string]any)
	for _, def := range []string{"component", "componentTarget", "componentExpose", "discoverRef"} {
		if _, ok := defs[def]; !ok {
			t.Errorf("fleet schema missing $defs.%s", def)
		}
	}
}

func TestWendySchemaJSON_NoLongerHasComponents(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(SchemaJSON), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if props, _ := schema["properties"].(map[string]any); props != nil {
		if _, ok := props["components"]; ok {
			t.Error("wendy.schema.json still has a 'components' property; it moved to wendy-fleet.schema.json")
		}
	}
}
