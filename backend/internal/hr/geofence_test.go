package hr

import (
	"math"
	"testing"
)

func TestDistanceM(t *testing.T) {
	// Dhaka (Shahbagh) to Dhaka (Farmgate): about 2.2 km.
	if d := distanceM(23.7383, 90.3958, 23.7571, 90.3893); math.Abs(d-2200) > 100 {
		t.Fatalf("distance = %.0f m", d)
	}
	if d := distanceM(23.75, 90.39, 23.75, 90.39); d != 0 {
		t.Fatalf("same point = %.3f m", d)
	}
}

func TestOfficeLocationValid(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		name string
		o    OfficeLocation
		want bool
	}{
		{"off and unset", OfficeLocation{RadiusM: 200}, true},
		{"on", OfficeLocation{Enabled: true, Lat: f(23.75), Lng: f(90.39), RadiusM: 200}, true},
		{"on without a place", OfficeLocation{Enabled: true, RadiusM: 200}, false},
		{"radius too small", OfficeLocation{Enabled: true, Lat: f(23.75), Lng: f(90.39), RadiusM: 5}, false},
		{"radius too big", OfficeLocation{Lat: f(23.75), Lng: f(90.39), RadiusM: 6000}, false},
		{"latitude out of range", OfficeLocation{Lat: f(91), Lng: f(90.39), RadiusM: 200}, false},
	} {
		if got := c.o.Valid(); got != c.want {
			t.Errorf("%s: Valid() = %v, want %v", c.name, got, c.want)
		}
	}
}
