package main

import (
	"reflect"
	"testing"
)

func TestAllowedOriginsCombinesConfiguredFrontendAndCORSOrigins(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://automation.mahadsaid.net/")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://metanicator.metanotech.com, https://preview.example.com/")

	want := []string{
		"https://automation.mahadsaid.net",
		"https://metanicator.metanotech.com",
		"https://preview.example.com",
	}
	if got := allowedOrigins(); !reflect.DeepEqual(got, want) {
		t.Fatalf("allowedOrigins() = %#v, want %#v", got, want)
	}
}

func TestAllowedOriginsDeduplicatesCaseInsensitively(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://Automation.Mahadsaid.net")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://automation.mahadsaid.net/")

	want := []string{"https://Automation.Mahadsaid.net"}
	if got := allowedOrigins(); !reflect.DeepEqual(got, want) {
		t.Fatalf("allowedOrigins() = %#v, want %#v", got, want)
	}
}

func TestWebsocketAllowedOriginsAddsLegacyWebsocketOnlyOrigins(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://desktop.example.com/, https://automation.mahadsaid.net")

	base := []string{"https://automation.mahadsaid.net"}
	want := []string{
		"https://automation.mahadsaid.net",
		"https://desktop.example.com",
	}
	if got := websocketAllowedOrigins(base); !reflect.DeepEqual(got, want) {
		t.Fatalf("websocketAllowedOrigins() = %#v, want %#v", got, want)
	}
}
