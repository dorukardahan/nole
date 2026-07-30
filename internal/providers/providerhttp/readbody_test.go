package providerhttp

import (
	"strings"
	"testing"
)

func TestReadAllLimitedUnderCapOK(t *testing.T) {
	body := strings.Repeat("a", 100)
	got, err := ReadAllLimited(strings.NewReader(body), 1000)
	if err != nil {
		t.Fatalf("ReadAllLimited under cap failed: %v", err)
	}
	if string(got) != body {
		t.Fatalf("got %d bytes, want %d", len(got), len(body))
	}
}

func TestReadAllLimitedExactlyAtCapOK(t *testing.T) {
	body := strings.Repeat("a", 64)
	got, err := ReadAllLimited(strings.NewReader(body), 64)
	if err != nil {
		t.Fatalf("ReadAllLimited at exact cap should succeed, got %v", err)
	}
	if len(got) != 64 {
		t.Fatalf("got %d bytes, want 64", len(got))
	}
}

func TestReadAllLimitedOverCapErrors(t *testing.T) {
	// Body content that, if leaked into the error, would be a secret disclosure.
	secret := strings.Repeat("SECRET-TOKEN-MUST-NOT-LEAK", 100)
	_, err := ReadAllLimited(strings.NewReader(secret), 16)
	if err == nil {
		t.Fatal("expected error when body exceeds cap")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exceeded 16 bytes") {
		t.Fatalf("error should report the cap, got %q", msg)
	}
	if strings.Contains(msg, "SECRET-TOKEN") {
		t.Fatalf("error leaked body content: %q", msg)
	}
}

func TestDecodeJSONLimitedUnderCapOK(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}
	if err := DecodeJSONLimited(strings.NewReader(`{"name":"nole"}`), 1000, &v); err != nil {
		t.Fatalf("DecodeJSONLimited under cap failed: %v", err)
	}
	if v.Name != "nole" {
		t.Fatalf("decoded name = %q, want nole", v.Name)
	}
}

func TestDecodeJSONLimitedOverCapErrors(t *testing.T) {
	// A large valid JSON document whose payload would leak secrets if echoed.
	big := `{"data":"` + strings.Repeat("SECRET-PAYLOAD", 1000) + `"}`
	var v map[string]string
	err := DecodeJSONLimited(strings.NewReader(big), 32, &v)
	if err == nil {
		t.Fatal("expected error when JSON body exceeds cap")
	}
	msg := err.Error()
	if !strings.Contains(msg, "too_large") {
		t.Fatalf("error should signal too_large, got %q", msg)
	}
	if strings.Contains(msg, "SECRET-PAYLOAD") {
		t.Fatalf("error leaked body content: %q", msg)
	}
}

func TestDecodeJSONLimitedExactlyAtCapOK(t *testing.T) {
	doc := `{"name":"x"}` // 12 bytes
	var v struct {
		Name string `json:"name"`
	}
	if err := DecodeJSONLimited(strings.NewReader(doc), int64(len(doc)), &v); err != nil {
		t.Fatalf("DecodeJSONLimited at exact cap should succeed, got %v", err)
	}
	if v.Name != "x" {
		t.Fatalf("decoded name = %q, want x", v.Name)
	}
}

func TestDecodeJSONLimitedRejectsTrailingMalformedData(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}
	err := DecodeJSONLimited(strings.NewReader(`{"name":"nole"} trailing-malformed`), 1000, &v)
	if err == nil {
		t.Fatal("valid JSON prefix plus malformed trailing data was accepted")
	}
}
