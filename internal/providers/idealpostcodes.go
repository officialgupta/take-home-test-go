package providers

import (
	"errors"
	"math/rand/v2"
	"time"
)

var ErrLookupFailed = errors.New("postcode lookup failed")

type GeoResult struct {
	Longitude float64
	Latitude  float64
}

// LookupPostcode mirrors reference-ts/src/providers/idealpostcodes.ts
func LookupPostcode(postcode string) (GeoResult, error) {
	success := rand.Float64() < 0.95

	time.Sleep(time.Second)

	if !success {
		return GeoResult{}, ErrLookupFailed
	}
	return GeoResult{Longitude: 50.05, Latitude: -5.05}, nil
}
