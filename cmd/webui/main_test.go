package main

import (
	"reflect"
	"testing"
)

func TestParseAllowedOrigins(t *testing.T) {
	t.Setenv("WEBUI_ALLOWED_ORIGINS", "https://Example.com, 192.168.10.124:8080,https://denysvitali.github.io/")

	got := configuredAllowedOrigins()
	want := []string{
		"192.168.10.124:8080",
		"denysvitali.github.io",
		"example.com",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredAllowedOrigins() = %#v, want %#v", got, want)
	}
}
