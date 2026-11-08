package hr

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/solvinx/opsflow/internal/apperr"
)

// Office location check: when HR switches it on, check-in and check-out are accepted
// only from within RadiusM metres of the office. The browser sends its position; it is
// not proof against a spoofed GPS, but it stops casual check-ins from home.

// OfficeLocation is the "office_location" setting.
type OfficeLocation struct {
	Enabled bool     `json:"enabled"`
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	RadiusM int      `json:"radius_m"`
}

// Valid reports whether the setting can be saved: a disabled one may leave the place
// unset, an enabled one needs real coordinates and a sensible radius.
func (o OfficeLocation) Valid() bool {
	if o.Lat != nil && (*o.Lat < -90 || *o.Lat > 90) || o.Lng != nil && (*o.Lng < -180 || *o.Lng > 180) {
		return false
	}
	if o.RadiusM < 0 || o.RadiusM > 5000 {
		return false
	}
	return !o.Enabled || o.Lat != nil && o.Lng != nil && o.RadiusM >= 20
}

// Position is where the device says it is; Accuracy is its error radius in metres.
type Position struct {
	Lat, Lng, Accuracy float64
}

// maxAccuracyAllowance caps how much of a fix's error radius is given the benefit of
// the doubt, so a coarse (IP-based) fix can't stretch the office to the whole city.
const maxAccuracyAllowance = 100

// Office reads the office location setting; a missing or unreadable one is off.
func (s *Service) Office(ctx context.Context) OfficeLocation {
	row, err := s.st.GetSetting(ctx, "office_location")
	var o OfficeLocation
	if err != nil || json.Unmarshal(row.Value, &o) != nil || !o.Valid() {
		return OfficeLocation{}
	}
	return o
}

// checkLocation returns the distance from the office in metres (-1 when the check is
// off), or an error when the position is missing or too far away.
func (s *Service) checkLocation(ctx context.Context, pos *Position, action string) (int, error) {
	o := s.Office(ctx)
	if !o.Enabled {
		return -1, nil
	}
	if pos == nil || pos.Lat < -90 || pos.Lat > 90 || pos.Lng < -180 || pos.Lng > 180 {
		return 0, apperr.BadRequest("Your location is needed to " + action + ". Allow location access in your browser and try again.")
	}
	d := int(math.Round(distanceM(*o.Lat, *o.Lng, pos.Lat, pos.Lng)))
	if float64(d)-math.Min(math.Max(pos.Accuracy, 0), maxAccuracyAllowance) > float64(o.RadiusM) {
		return d, &apperr.Error{Status: http.StatusForbidden, Code: "outside_office", Message: fmt.Sprintf(
			"You are %s from the office (allowed: %d m), so you can't %s here. If you were working, request a correction.", humanDistance(d), o.RadiusM, action)}
	}
	return d, nil
}

// distanceM is the great-circle (haversine) distance between two points in metres.
func distanceM(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadius = 6371000.0
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(a)))
}

func humanDistance(m int) string {
	if m < 1000 {
		return fmt.Sprintf("%d m", m)
	}
	return fmt.Sprintf("%.1f km", float64(m)/1000)
}
