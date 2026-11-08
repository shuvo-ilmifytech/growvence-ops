package hr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"

	"github.com/solvinx/opsflow/internal/apperr"
)

// Office network check: HR and the CEO list the office's public IPs (single addresses
// or CIDR ranges such as 103.220.204.0/24). Every check-in and out records whether it
// came from one of them; when the check is switched on, others are refused.

// OfficeNetwork is the "office_network" setting.
type OfficeNetwork struct {
	Enabled bool     `json:"enabled"`
	IPs     []string `json:"ips"`
}

const maxOfficeIPs = 50

func parseNet(s string) (netip.Prefix, bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err == nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, false
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), true
}

// Valid reports whether the setting can be saved: every entry is an IP or a CIDR range,
// and an enabled check names at least one.
func (o OfficeNetwork) Valid() bool {
	if len(o.IPs) > maxOfficeIPs || o.Enabled && len(o.IPs) == 0 {
		return false
	}
	for _, s := range o.IPs {
		if _, ok := parseNet(s); !ok {
			return false
		}
	}
	return true
}

// Contains reports whether ip is one of the office addresses.
func (o OfficeNetwork) Contains(ip string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, s := range o.IPs {
		if p, ok := parseNet(s); ok && p.Contains(a) {
			return true
		}
	}
	return false
}

// OfficeNetwork reads the office network setting; a missing or unreadable one is off.
func (s *Service) OfficeNetwork(ctx context.Context) OfficeNetwork {
	row, err := s.st.GetSetting(ctx, "office_network")
	var o OfficeNetwork
	if err != nil || json.Unmarshal(row.Value, &o) != nil || !o.Valid() {
		return OfficeNetwork{}
	}
	return o
}

// checkNetwork says whether ip is on the office network (nil when no office IPs are
// set), or refuses the check-in/out when the check is on and it isn't.
func (s *Service) checkNetwork(ctx context.Context, ip, action string) (*bool, error) {
	o := s.OfficeNetwork(ctx)
	if len(o.IPs) == 0 {
		return nil, nil
	}
	on := o.Contains(ip)
	if o.Enabled && !on {
		return &on, &apperr.Error{Status: http.StatusForbidden, Code: "outside_office_network", Message: "You're not on the office Wi-Fi, so you can't " +
			action + ". Connect to the office Wi-Fi (not mobile data) and try again. If you were working, request a correction."}
	}
	return &on, nil
}
