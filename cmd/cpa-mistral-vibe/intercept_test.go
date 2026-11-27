package main

import (
	"encoding/json"
	"regexp"
	"testing"
)

// metadataIn returns the metadata object of a rewritten body, failing the
// test when it is missing or not a JSON object.
func metadataIn(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	meta, ok := doc["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("body has no metadata object: %s", body)
	}
	return meta
}

func TestVibeMetadataInjected(t *testing.T) {
	body := []byte(`{"model":"mistral-large-latest","messages":[{"role":"user","content":"hi"}]}`)
	out, changed := applyRequestFixes(body)
	if !changed {
		t.Fatal("a body without metadata should change")
	}
	want := map[string]string{
		"agent_entrypoint": "programmatic",
		"client_name":      "vibe_programmatic",
		"call_source":      "vibe_code",
		"call_type":        "main_call",
		"host_kind":        "local",
		"user_plan":        "individual",
		"version":          "3.1.3",
	}
	meta := metadataIn(t, out)
	for k, v := range want {
		if meta[k] != v {
			t.Errorf("metadata.%s = %v, want %q", k, meta[k], v)
		}
	}
	sid, _ := meta["session_id"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(sid) {
		t.Errorf("session_id %q is not a v4 uuid", sid)
	}
	if _, again := applyRequestFixes(out); again {
		t.Error("a second pass changed an already annotated body")
	}
}

func TestVibeMetadataMergesClientKeys(t *testing.T) {
	body := []byte(`{"metadata":{"foo":"bar","call_source":"client_controlled"}}`)
	out, changed := applyRequestFixes(body)
	if !changed {
		t.Fatal("a body with partial metadata should change")
	}
	meta := metadataIn(t, out)
	if meta["foo"] != "bar" {
		t.Errorf("client metadata key lost: %v", meta)
	}
	if meta["call_source"] != "vibe_code" {
		t.Errorf("client overrode billing routing: call_source = %v", meta["call_source"])
	}
}

func TestVibeMetadataReplacesWrongType(t *testing.T) {
	for _, body := range []string{
		`{"metadata":"vibe"}`,
		`{"metadata":3}`,
		`{"metadata":["vibe"]}`,
		`{"metadata":null}`,
	} {
		out, changed := applyRequestFixes([]byte(body))
		if !changed {
			t.Errorf("%s should change", body)
			continue
		}
		if meta := metadataIn(t, out); meta["call_source"] != "vibe_code" {
			t.Errorf("%s: call_source = %v", body, meta["call_source"])
		}
	}
}

func TestApplyRequestFixesInvalidJSON(t *testing.T) {
	if out, changed := applyRequestFixes([]byte(`{"messages":`)); changed || out != nil {
		t.Errorf("invalid JSON: got changed=%v out=%s, want false/nil", changed, out)
	}
}
