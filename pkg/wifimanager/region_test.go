package wifimanager

import "testing"

func TestParseCountryIE(t *testing.T) {
	ssid := []byte{0, 2, 'h', 'i'}
	cases := map[string][]byte{
		"DE": append(append([]byte{}, ssid...), 7, 6, 'd', 'e', ' ', 1, 2, 3),
		"":   append(append([]byte{}, ssid...), 7, 3, 'X', 'X', ' '),
	}
	for want, ies := range cases {
		if got := parseCountryIE(ies); got != want {
			t.Errorf("parseCountryIE = %q, want %q", got, want)
		}
	}
	for _, ies := range [][]byte{nil, {7, 5, 'D'}, {7, 3, '1', '2', ' '}, ssid} {
		if got := parseCountryIE(ies); got != "" {
			t.Errorf("parseCountryIE(%v) = %q, want empty", ies, got)
		}
	}
}

func TestInferCountry(t *testing.T) {
	obs := []countryObservation{
		{"a", "DE", -6000}, {"a", "DE", -5000}, // same AP seen twice counts once
		{"b", "DE", -7000},
		{"c", "US", -3000},
	}
	got := inferCountry(obs)
	if got.Country != "DE" || got.Votes != 2 || got.Total != 3 || !got.Confident() {
		t.Fatalf("got %+v", got)
	}

	single := inferCountry([]countryObservation{{"a", "US", -4000}})
	if single.Country != "US" || single.Confident() {
		t.Fatalf("single AP must not be confident: %+v", single)
	}

	tie := inferCountry([]countryObservation{{"a", "DE", -7000}, {"b", "FR", -4000}})
	if tie.Country != "FR" || tie.Confident() {
		t.Fatalf("tie should favour stronger signal without confidence: %+v", tie)
	}

	if got := inferCountry(nil); got.Country != "" || got.Confident() {
		t.Fatalf("empty: %+v", got)
	}
}
