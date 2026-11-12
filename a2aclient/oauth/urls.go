// Copyright 2026 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package oauth

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	wellKnownServerMetadata   = "/.well-known/oauth-authorization-server"
	wellKnownOpenIDConfig     = "/.well-known/openid-configuration"
	wellKnownResourceMetadata = "/.well-known/oauth-protected-resource"

	// maxResourcePathSegments bounds the path segments of a request URL that
	// are tried as the resource path when probing for Protected Resource Metadata.
	maxResourcePathSegments = 4
	// maxAuthorizationServers bounds the authorization servers of Protected
	// Resource Metadata that are tried.
	maxAuthorizationServers = 5
	// maxCardMetadataDocuments bounds the security schemes of an agent card
	// whose authorization server metadata is fetched.
	maxCardMetadataDocuments = 5
)

// NormalizeResource returns the canonical form of an agent URL used as an RFC
// 8707 resource indicator and grant-store key: lowercase scheme and host, no
// default port, query, fragment or trailing slash. gRPC "host:port" addresses
// get an https scheme. Input that doesn't parse as a URL with a host is returned
// unchanged.
func NormalizeResource(raw string) string {
	if raw == "" {
		return ""
	}
	withScheme := raw
	if !strings.Contains(raw, "://") {
		withScheme = "https://" + raw
	}
	u, err := url.Parse(withScheme)
	if err != nil || u.Host == "" {
		return raw
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host + strings.TrimSuffix(u.EscapedPath(), "/")
}

// sameIssuer compares issuer identifiers, ignoring a trailing slash.
func sameIssuer(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// sameEndpoint compares endpoint URLs in their normalized form.
func sameEndpoint(a, b string) bool {
	return NormalizeResource(a) == NormalizeResource(b)
}

// origin returns scheme://host[:port] of a normalized URL.
func origin(raw string) string {
	u, err := url.Parse(NormalizeResource(raw))
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ResourceCovers reports whether a grant for resource applies to requests to
// target: same origin, and the target path is the resource path or below it.
func ResourceCovers(resource, target string) bool {
	return resourceCovers(resource, target)
}

// resourceCovers implements [ResourceCovers]. It also stands in for the exact
// resource identity check of RFC 9728 §3.3 when Protected Resource Metadata is
// accepted, which is a deliberate relaxation: a document for an agent's base URL
// describes requests to every path below it. A path with dot segments covers
// nothing and is covered by nothing, since a server resolves them and the prefix
// comparison would not.
func resourceCovers(resource, target string) bool {
	r, t := NormalizeResource(resource), NormalizeResource(target)
	if origin(r) != origin(t) {
		return false
	}
	rp := strings.TrimPrefix(r, origin(r))
	tp := strings.TrimPrefix(t, origin(t))
	if hasDotSegment(rp) || hasDotSegment(tp) {
		return false
	}
	return rp == "" || tp == rp || strings.HasPrefix(tp, rp+"/")
}

// hasDotSegment reports whether an escaped path has a "." or ".." segment, also
// when percent-encoded or behind an encoded slash or a backslash.
func hasDotSegment(escapedPath string) bool {
	for seg := range strings.SplitSeq(escapedPath, "/") {
		unescaped, err := url.PathUnescape(seg)
		if err != nil {
			return true
		}
		for _, part := range strings.FieldsFunc(unescaped, func(r rune) bool { return r == '/' || r == '\\' }) {
			if part == "." || part == ".." {
				return true
			}
		}
	}
	return false
}

// checkSecureURL requires https, allowing http only for loopback hosts so local
// agents and tests work.
func checkSecureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	switch {
	case u.Host == "":
		return fmt.Errorf("invalid URL %q: missing host", raw)
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	default:
		return fmt.Errorf("refusing non-https URL %q", raw)
	}
}

// isLoopbackURL reports whether a URL, or a gRPC "host:port" address, may
// reach the local machine: a loopback host, or a numeric host in a form
// net.ParseIP doesn't read, such as 127.1, which resolvers may still turn into
// a loopback address. It decides what is refused for agents on other hosts, so
// it errs on the side of loopback.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(NormalizeResource(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	return isLoopback(host) || unusualNumericHost(host)
}

// unusualNumericHost reports whether host looks like an IPv4 address in a
// shortened, decimal, octal or hex form, such as 127.1 or 0x7f000001.
func unusualNumericHost(host string) bool {
	if host == "" || host[0] < '0' || host[0] > '9' || net.ParseIP(host) != nil {
		return false
	}
	return strings.Trim(strings.ToLower(host), "0123456789abcdefx.") == ""
}

// isLoopback reports whether host names the local machine: localhost or one
// of its subdomains (RFC 6761), with or without a trailing dot, a loopback
// address, or the unspecified address, which also reaches the local machine.
func isLoopback(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// serverMetadataURLs returns where an issuer's metadata may live: the RFC 8414
// location, with the well-known segment inserted before the issuer path; for
// issuers with a path, the RFC 8414 segment appended to it, which Okta and
// Keycloak serve; and the OpenID Connect Discovery location, with its segment
// appended.
func serverMetadataURLs(issuer string) []string {
	u, err := url.Parse(strings.TrimSuffix(issuer, "/"))
	if err != nil || u.Host == "" {
		return nil
	}
	base := u.Scheme + "://" + u.Host
	path := u.EscapedPath()
	urls := []string{base + wellKnownServerMetadata + path}
	if path != "" {
		urls = append(urls, base+path+wellKnownServerMetadata)
	}
	return append(urls, base+path+wellKnownOpenIDConfig)
}

// resourceMetadataURLs returns the RFC 9728 well-known locations for a request
// URL, which may be below the resource: with each ancestor of the request path
// inserted, longest first, then the origin root. Only the first
// maxResourcePathSegments path segments are used.
func resourceMetadataURLs(target *url.URL) []string {
	base := target.Scheme + "://" + target.Host + wellKnownResourceMetadata
	var segments []string
	if path := strings.Trim(target.EscapedPath(), "/"); path != "" {
		segments = strings.Split(path, "/")
	}
	segments = segments[:min(len(segments), maxResourcePathSegments)]
	urls := make([]string, 0, len(segments)+1)
	for i := len(segments); i > 0; i-- {
		urls = append(urls, base+"/"+strings.Join(segments[:i], "/"))
	}
	return append(urls, base)
}
