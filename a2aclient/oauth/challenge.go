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
	"strings"
)

// Challenge is one authentication challenge from a WWW-Authenticate header.
type Challenge struct {
	// Scheme is the lowercase authentication scheme, e.g. "bearer".
	Scheme string
	// Params holds the auth-params with lowercase names.
	Params map[string]string
}

// ParseChallenges parses every challenge in the given WWW-Authenticate header
// values (RFC 9110 §11.6.1). A header may carry several comma-separated challenges.
func ParseChallenges(values []string) []Challenge {
	var out []Challenge
	for _, v := range values {
		out = append(out, parseChallengeList(v)...)
	}
	return out
}

func parseChallengeList(s string) []Challenge {
	var out []Challenge
	p := &challengeParser{s: s}
	var cur *Challenge
	for {
		p.skip(" \t,")
		if p.done() {
			break
		}
		tok := p.token()
		if tok == "" {
			p.i++
			continue
		}
		p.skip(" \t")
		if !p.done() && p.peek() == '=' && cur != nil {
			p.i++
			p.skip(" \t")
			cur.Params[strings.ToLower(tok)] = p.value()
			continue
		}
		out = append(out, Challenge{Scheme: strings.ToLower(tok), Params: map[string]string{}})
		cur = &out[len(out)-1]
		if p.token68() {
			continue
		}
	}
	return out
}

type challengeParser struct {
	s string
	i int
}

func (p *challengeParser) done() bool { return p.i >= len(p.s) }
func (p *challengeParser) peek() byte { return p.s[p.i] }

func (p *challengeParser) skip(chars string) {
	for !p.done() && strings.IndexByte(chars, p.peek()) >= 0 {
		p.i++
	}
}

func (p *challengeParser) token() string {
	start := p.i
	for !p.done() && isTokenChar(p.peek()) {
		p.i++
	}
	return p.s[start:p.i]
}

// token68 consumes a token68 credential following a scheme (e.g. "Negotiate abc==")
// so it is not mistaken for a parameter name. It reports whether one was consumed.
func (p *challengeParser) token68() bool {
	save := p.i
	tok := p.token()
	if tok == "" {
		return false
	}
	end := p.i
	for !p.done() && p.peek() == '=' {
		p.i++
	}
	p.skip(" \t")
	if p.done() || p.peek() == ',' {
		if end == p.i {
			p.i = save
			return false
		}
		return true
	}
	p.i = save
	return false
}

func (p *challengeParser) value() string {
	if p.done() {
		return ""
	}
	if p.peek() != '"' {
		return p.token()
	}
	p.i++
	var b strings.Builder
	for !p.done() {
		c := p.peek()
		p.i++
		switch c {
		case '\\':
			if !p.done() {
				b.WriteByte(p.peek())
				p.i++
			}
		case '"':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isTokenChar(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~/:", c) >= 0
}

// bearerChallenge returns the first Bearer challenge.
func bearerChallenge(chs []Challenge) (Challenge, bool) {
	for _, c := range chs {
		if c.Scheme == "bearer" {
			return c, true
		}
	}
	return Challenge{}, false
}

// splitScopes splits a space-separated scope string.
func splitScopes(s string) []string {
	if f := strings.Fields(s); len(f) > 0 {
		return f
	}
	return nil
}
