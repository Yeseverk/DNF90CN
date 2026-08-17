package httpx

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var DefaultClientIPHeaders = []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"}

type ClientIPOptions struct {
	Headers        []string
	TrustedProxies []string
}

type IPInfo struct {
	IP      string `json:"ip"`
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
	City    string `json:"city,omitempty"`
	ASN     string `json:"asn,omitempty"`
	Source  string `json:"source,omitempty"`
}

type IPLocator interface {
	LookupIP(context.Context, string) (IPInfo, bool, error)
}

type StaticIPRange struct {
	CIDR    string `json:"cidr"`
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
	City    string `json:"city,omitempty"`
	ASN     string `json:"asn,omitempty"`
	Source  string `json:"source,omitempty"`
}

type StaticIPLocator struct {
	ranges []staticIPRange
}

type staticIPRange struct {
	prefix netip.Prefix
	info   IPInfo
}

func NewStaticIPLocator(ranges []StaticIPRange) (*StaticIPLocator, error) {
	out := make([]staticIPRange, 0, len(ranges))
	for _, r := range ranges {
		cidr := strings.TrimSpace(r.CIDR)
		if cidr == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, err
		}
		out = append(out, staticIPRange{
			prefix: prefix,
			info: IPInfo{
				Country: strings.TrimSpace(r.Country),
				Region:  strings.TrimSpace(r.Region),
				City:    strings.TrimSpace(r.City),
				ASN:     strings.TrimSpace(r.ASN),
				Source:  strings.TrimSpace(r.Source),
			},
		})
	}
	return &StaticIPLocator{ranges: out}, nil
}

func (l *StaticIPLocator) LookupIP(ctx context.Context, ip string) (IPInfo, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return IPInfo{}, false, err
	}
	addr, ok := parseIPAddr(ip)
	if !ok || l == nil {
		return IPInfo{}, false, nil
	}
	for _, r := range l.ranges {
		if !r.prefix.Contains(addr) {
			continue
		}
		info := r.info
		info.IP = addr.String()
		return info, true, nil
	}
	return IPInfo{}, false, nil
}

func ClientIP(r *http.Request, options ClientIPOptions) string {
	if r == nil {
		return ""
	}
	remote := RemoteAddrIP(r.RemoteAddr)
	if remote == "" {
		return ""
	}
	if !remoteTrusted(remote, options.TrustedProxies) {
		return remote
	}
	for _, header := range clientIPHeaders(options.Headers) {
		for _, candidate := range strings.Split(r.Header.Get(header), ",") {
			if ip := normalizeIP(candidate); ip != "" {
				return ip
			}
		}
	}
	return remote
}

func RemoteAddrIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(remoteAddr)
}

func clientIPHeaders(headers []string) []string {
	if len(headers) == 0 {
		return DefaultClientIPHeaders
	}
	out := make([]string, 0, len(headers))
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if header != "" {
			out = append(out, header)
		}
	}
	if len(out) == 0 {
		return DefaultClientIPHeaders
	}
	return out
}

func remoteTrusted(remote string, cidrs []string) bool {
	if len(cidrs) == 0 {
		return false
	}
	addr, ok := parseIPAddr(remote)
	if !ok {
		return false
	}
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			continue
		}
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func normalizeIP(value string) string {
	addr, ok := parseIPAddr(value)
	if !ok {
		return ""
	}
	return addr.String()
}

func parseIPAddr(value string) (netip.Addr, bool) {
	value = strings.Trim(strings.TrimSpace(value), "[]")
	if value == "" {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
