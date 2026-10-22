package wifimanager

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/mdlayher/wifi"
)

const informationElementCountry = 7

// countryObservation is the 802.11d country advertised by one access point.
type countryObservation struct {
	BSSID   string
	Country string
	Signal  int32 // mBm
}

// RegionDetection is the result of inferring the regulatory region from the
// country information elements of nearby access points.
type RegionDetection struct {
	Country string `json:"country"` // empty when nothing usable was seen
	Votes   int    `json:"votes"`   // access points advertising Country
	Total   int    `json:"total"`   // access points advertising any country
}

// Confident reports whether enough independent access points agree to apply
// the region automatically. A single AP can be misconfigured (many ship with
// a hard-coded "US"), so a lone observation is only a suggestion.
func (d RegionDetection) Confident() bool {
	return d.Country != "" && d.Votes >= 2 && d.Votes*2 > d.Total
}

// parseCountryIE extracts the two-letter country from a country element
// (id 7). "XX" (non-country) and non-letter values are rejected.
func parseCountryIE(ies []byte) string {
	for len(ies) >= 2 {
		id, length := ies[0], int(ies[1])
		ies = ies[2:]
		if length > len(ies) {
			return ""
		}
		data := ies[:length]
		ies = ies[length:]
		if id != informationElementCountry || len(data) < 2 {
			continue
		}
		country := strings.ToUpper(string(data[:2]))
		for _, r := range country {
			if r < 'A' || r > 'Z' {
				return ""
			}
		}
		if country == "XX" {
			return ""
		}
		return country
	}
	return ""
}

// inferCountry picks the country advertised by the most distinct access
// points; ties go to the country with the strongest signal.
func inferCountry(observations []countryObservation) RegionDetection {
	type tally struct {
		bssids map[string]struct{}
		best   int32
	}
	tallies := map[string]*tally{}
	seen := map[string]struct{}{}
	for _, o := range observations {
		if o.Country == "" {
			continue
		}
		t := tallies[o.Country]
		if t == nil {
			t = &tally{bssids: map[string]struct{}{}, best: o.Signal}
			tallies[o.Country] = t
		}
		t.bssids[o.BSSID] = struct{}{}
		seen[o.BSSID] = struct{}{}
		if o.Signal > t.best {
			t.best = o.Signal
		}
	}
	if len(tallies) == 0 {
		return RegionDetection{}
	}

	countries := make([]string, 0, len(tallies))
	for c := range tallies {
		countries = append(countries, c)
	}
	sort.Slice(countries, func(i, j int) bool {
		a, b := tallies[countries[i]], tallies[countries[j]]
		if len(a.bssids) != len(b.bssids) {
			return len(a.bssids) > len(b.bssids)
		}
		if a.best != b.best {
			return a.best > b.best
		}
		return countries[i] < countries[j]
	})
	top := countries[0]
	return RegionDetection{Country: top, Votes: len(tallies[top].bssids), Total: len(seen)}
}

// DetectRegion scans for nearby access points and infers the regulatory
// region from the country elements they advertise.
func (m *Manager) DetectRegion() (RegionDetection, error) {
	cl, err := wifi.New()
	if err != nil {
		return RegionDetection{}, fmt.Errorf("WiFi client unavailable: %w", err)
	}
	defer cl.Close()

	interfaces, err := cl.Interfaces()
	if err != nil {
		return RegionDetection{}, fmt.Errorf("no WiFi interfaces: %w", err)
	}

	var observations []countryObservation
	var lastErr error
	for _, intf := range interfaces {
		ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
		if err := triggerScan(ctx, cl, intf); err != nil {
			// Cached results are still usable for inference.
			log.Printf("Region detection: scan on %s failed: %v; using cached results", intf.Name, err)
		} else if scanWaitDelay > 0 {
			time.Sleep(scanWaitDelay)
		}
		cancel()

		obs, err := readCountryObservations(intf)
		if err != nil {
			lastErr = err
			continue
		}
		observations = append(observations, obs...)
	}
	if len(observations) == 0 && lastErr != nil {
		return RegionDetection{}, lastErr
	}
	return inferCountry(observations), nil
}
