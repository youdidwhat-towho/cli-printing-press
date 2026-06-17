package spec

import "testing"

// These tests lock in the poison-tool-lockout fix: a param whose only name is
// a raw wire name with characters illegal in an MCP tool input-schema property
// key (e.g. "assignees[]") must never surface that raw name as its public
// input name, because one such key makes the Anthropic API reject every
// request in a session that loads the tool.

func TestSanitizeMCPPropertyKey(t *testing.T) {
	cases := map[string]string{
		"assignees[]":      "assignees",
		"custom_items[]":   "custom_items",
		"filters[status]":  "filtersstatus",
		"start_date":       "start_date", // already legal — unchanged
		"order.by":         "order.by",   // dot is legal — unchanged
		"page":             "page",
		"?weird=&chars[]!": "weirdchars",
		"[]":               "", // nothing usable remains
	}
	for in, want := range cases {
		if got := sanitizeMCPPropertyKey(in); got != want {
			t.Errorf("sanitizeMCPPropertyKey(%q) = %q, want %q", in, got, want)
		}
	}
	// Length is capped at 64.
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	if got := sanitizeMCPPropertyKey(string(long)); len(got) != 64 {
		t.Errorf("sanitizeMCPPropertyKey(100 a's) length = %d, want 64", len(got))
	}
}

func TestPublicInputNameSanitizesRawWireFallback(t *testing.T) {
	p := Param{Name: "assignees[]"}
	got := p.PublicInputName()
	if got != "assignees" {
		t.Fatalf("PublicInputName() = %q, want %q", got, "assignees")
	}
	if !mcpPropertyKeyRe.MatchString(got) {
		t.Fatalf("PublicInputName() = %q does not match the MCP property-key pattern", got)
	}
}

func TestValidatePublicParamNamesGuardsMCPKey(t *testing.T) {
	// A param that sanitizes to a valid key passes.
	if err := validatePublicParamNameList("params", []Param{{Name: "assignees[]"}}); err != nil {
		t.Fatalf("expected sanitizable wire name to pass, got: %v", err)
	}
	// A param whose name has no usable characters cannot produce a valid key,
	// so the generation-time guard must fail loudly rather than ship a brick.
	if err := validatePublicParamNameList("params", []Param{{Name: "[]"}}); err == nil {
		t.Fatal("expected guard to reject a param with no valid derivable MCP key, got nil")
	}
}
