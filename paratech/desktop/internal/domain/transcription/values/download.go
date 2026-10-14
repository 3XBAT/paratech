package values

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Bounds of the download host allowlist.
const (
	MaxDownloadHosts = 32
	maxHostLen       = 253
	maxHostPortLen   = maxHostLen + 2 + 6 // brackets and ":65535"
	defaultHTTPSPort = 443
	maxPort          = 65535
)

// DiskSpaceMargin is kept free on top of the bytes a download still needs, so that a download never fills the disk
// that the database and recordings share (PRD section 8: warn about required disk space before download).
const DiskSpaceMargin int64 = 64 << 20

// HostAllowlist is the set of hosts a model may be downloaded from (PRD sections 8.1 and 13.2: no arbitrary
// outbound URLs). Hosts are normalized: lowercase, no trailing dot, no default https port. It is immutable.
type HostAllowlist struct{ hosts map[string]bool }

// NewHostAllowlist normalizes and validates the hosts; an empty list allows nothing.
func NewHostAllowlist(hosts []string) (HostAllowlist, error) {
	if len(hosts) > MaxDownloadHosts {
		return HostAllowlist{}, fmt.Errorf("%w: more than %d download hosts", ErrInvalidHost, MaxDownloadHosts)
	}

	set := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		n, err := NormalizeHost(h)
		if err != nil {
			return HostAllowlist{}, err
		}
		set[n] = true
	}
	return HostAllowlist{hosts: set}, nil
}

// Hosts returns the normalized hosts, sorted.
func (a HostAllowlist) Hosts() []string {
	out := make([]string, 0, len(a.hosts))
	for h := range a.hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// with returns a copy that also allows the given normalized hosts.
func (a HostAllowlist) with(hosts ...string) HostAllowlist {
	set := make(map[string]bool, len(a.hosts)+len(hosts))
	for h := range a.hosts {
		set[h] = true
	}
	for _, h := range hosts {
		set[h] = true
	}
	return HostAllowlist{hosts: set}
}

// Check reports whether a download (or a redirect of one) may request rawURL: an absolute https URL without user
// info whose normalized host is in the list. It returns ErrHostNotAllowed for a host outside the list and
// ErrInvalidSourceURL for anything that is not such a URL.
func (a HostAllowlist) Check(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSourceURL, err)
	}
	switch {
	case !strings.EqualFold(u.Scheme, schemeHTTPS):
		return fmt.Errorf("%w: scheme %q, only https is allowed", ErrInvalidSourceURL, u.Scheme)
	case u.Opaque != "":
		return fmt.Errorf("%w: not a hierarchical URL", ErrInvalidSourceURL)
	case u.User != nil:
		return fmt.Errorf("%w: user info is not allowed", ErrInvalidSourceURL)
	}

	host, err := NormalizeHost(u.Host)
	if err != nil {
		return err
	}
	if !a.hosts[host] {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
	}
	return nil
}

// NormalizeHost returns the comparable form of a URL host with an optional port: lowercase, no trailing dot of a
// fully qualified name, no default https port, the port without leading zeros. IPv6 literals keep their brackets.
// Anything but ASCII letters, digits, '-' and '.' in a name (or hex digits, ':' and '.' in an IPv6 literal), an
// empty label and a bad port are refused with ErrInvalidHost.
func NormalizeHost(hostport string) (string, error) {
	if hostport == "" || len(hostport) > maxHostPortLen || strings.HasSuffix(hostport, ":") {
		return "", fmt.Errorf("%w: %q", ErrInvalidHost, hostport)
	}

	u := url.URL{Host: hostport}
	host, port := u.Hostname(), u.Port()
	if strings.HasPrefix(hostport, "[") {
		if !validIPv6Literal(host) {
			return "", fmt.Errorf("%w: %q", ErrInvalidHost, hostport)
		}
		host = "[" + strings.ToLower(host) + "]"
	} else {
		host = strings.TrimSuffix(strings.ToLower(host), ".")
		if !validHostName(host) {
			return "", fmt.Errorf("%w: %q", ErrInvalidHost, hostport)
		}
	}

	if port == "" {
		return host, nil
	}
	n, ok := parsePort(port)
	if !ok {
		return "", fmt.Errorf("%w: %q: bad port", ErrInvalidHost, hostport)
	}
	if n == defaultHTTPSPort {
		return host, nil
	}
	return host + ":" + strconv.Itoa(n), nil
}

func validHostName(h string) bool {
	if h == "" || len(h) > maxHostLen {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !isLowerAlnum(r) && r != '-' {
				return false
			}
		}
	}
	return true
}

func validIPv6Literal(s string) bool {
	if !strings.Contains(s, ":") {
		return false
	}
	for _, r := range s {
		hex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !hex && r != ':' && r != '.' {
			return false
		}
	}
	return true
}

func parsePort(p string) (int, bool) {
	if len(p) > 5 {
		return 0, false
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > maxPort {
		return 0, false
	}
	return n, true
}

// RangeStatus is how a server answered a request for a model file from an offset.
type RangeStatus int

// Server answers to a ranged request.
const (
	// RangeFull is a 200 reply: the whole file from byte 0, the Range header was ignored.
	RangeFull RangeStatus = iota + 1
	// RangePartial is a 206 reply with a Content-Range.
	RangePartial
	// RangeNotSatisfiable is a 416 reply: the server has nothing at the offset.
	RangeNotSatisfiable
)

// RangeReply is the transport-neutral form of a reply to a ranged download request.
type RangeReply struct {
	Status RangeStatus
	Start  int64 // first byte of the body; 0 for RangeFull
	Total  int64 // length of the whole file; UnknownTotal if the server did not say
}

// UnknownTotal marks a RangeReply whose server did not state the file length.
const UnknownTotal int64 = -1

// ResumeStep is what a download does with a reply (PRD section 8: resume support).
type ResumeStep int

// Steps of PlanResume.
const (
	// StepAppend appends the body to the partial file.
	StepAppend ResumeStep = iota + 1
	// StepReplace discards the partial file and writes the body from byte 0.
	StepReplace
	// StepRestart discards the partial file and requests the file again from byte 0; the body is not used.
	StepRestart
)

// ResumeOffset returns where a download continues when have bytes of the file are already stored: have itself when
// it fits the file, otherwise 0 (the partial file is longer than the model and is discarded). complete reports that
// the stored bytes already have the full size, so only the checksum is left.
func (e CatalogEntry) ResumeOffset(have int64) (offset int64, complete bool) {
	if have < 0 || have > e.size {
		return 0, false
	}
	return have, have == e.size
}

// PlanResume decides on the reply to a request from offset (PRD section 8: resumable download). A file length that
// differs from the catalog is ErrSizeMismatch: the source serves another file. A reply that does not continue at
// offset is StepRestart, except when the request already started at 0, which is ErrUnexpectedRange.
func (e CatalogEntry) PlanResume(offset int64, r RangeReply) (ResumeStep, error) {
	if r.Total != UnknownTotal && r.Total != e.size {
		return 0, fmt.Errorf("%w: %s %s: source has %d bytes, want %d", ErrSizeMismatch, e.name, e.version, r.Total, e.size)
	}

	switch r.Status {
	case RangeFull:
		return StepReplace, nil
	case RangePartial:
		if r.Start == offset {
			return StepAppend, nil
		}
	case RangeNotSatisfiable:
	default:
		return 0, fmt.Errorf("%w: unknown reply status %d", ErrUnexpectedRange, r.Status)
	}

	if offset == 0 {
		return 0, fmt.Errorf("%w: %s %s: reply does not start at byte 0", ErrUnexpectedRange, e.name, e.version)
	}
	return StepRestart, nil
}

// RequiredSpace returns the free disk space a download needs when have bytes are stored: the rest of the file plus
// DiskSpaceMargin.
func (e CatalogEntry) RequiredSpace(have int64) int64 {
	offset, _ := e.ResumeOffset(have)
	return e.size - offset + DiskSpaceMargin
}

// CheckDiskSpace reports whether free bytes are enough to download the rest of the file (PRD section 8: warn about
// required disk space before download). Too little space is an *InsufficientSpaceError.
func (e CatalogEntry) CheckDiskSpace(have, free int64) error {
	if need := e.RequiredSpace(have); free < need {
		return &InsufficientSpaceError{Required: need, Available: free}
	}
	return nil
}

// InsufficientSpaceError is the disk space warning shown to the user before a download (PRD section 8). It wraps
// ErrInsufficientSpace.
type InsufficientSpaceError struct {
	Required  int64 // bytes the download needs, margin included
	Available int64 // free bytes on the disk of the data directory
}

func (e *InsufficientSpaceError) Error() string {
	return fmt.Sprintf("%v: %d bytes needed, %d available", ErrInsufficientSpace, e.Required, e.Available)
}

// Unwrap returns ErrInsufficientSpace.
func (e *InsufficientSpaceError) Unwrap() error { return ErrInsufficientSpace }
