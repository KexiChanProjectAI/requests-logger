package proxy

import (
	"regexp"
	"strings"
)
// denyExact are paths that are explicitly denied with exact matching.
var denyExact = []string{
	"/admin",
}

// denyPrefixes are path prefixes that are denied.
var denyPrefixes = []string{
	"/admin/",
	"/api/v1/admin/",
	"/api/v1/auth/",
	"/api/v1/user/",
	"/api/v1/keys/",
	"/api/v1/payment/",
	"/api/v1/pages/",
}

// allowExact are paths that are explicitly allowed with exact matching.
var allowExact = []string{
	"/responses",
	"/chat/completions",
	"/embeddings",
}

// allowPrefixes are path prefixes that are allowed.
var allowPrefixes = []string{
	"/v1/",
	"/v1beta/",
	"/images/",
	"/backend-api/codex/",
	"/antigravity/",
}

// shouldLogPath returns true if the given path should be logged based on
// the hardcoded route policy rules. Denies are evaluated first, then exact
// AI routes, then AI prefixes, with a default of false.
func shouldLogPath(path string) bool {
	// Step 1: Check deny exact matches
	for _, p := range denyExact {
		if path == p {
			return false
		}
	}

	// Step 2: Check deny prefixes
	for _, prefix := range denyPrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}

	// Step 3: Check allow exact matches (AI routes)
	for _, p := range allowExact {
		if path == p {
			return true
		}
	}

	// Step 4: Check allow prefixes (AI prefixes)
	for _, prefix := range allowPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	// Step 5: Default - do not log
	return false
}

// shouldBlockUA returns true if the User-Agent should be blocked based on
// the configured whitelist and blacklist regex patterns.
// Whitelist takes precedence: if whitelist is non-empty, only UAs matching
// at least one whitelist pattern are allowed (blacklist is ignored).
// If only blacklist is set, any UA matching a blacklist pattern is blocked.
// If neither is set, all UAs are allowed.
func shouldBlockUA(ua string, whitelist, blacklist []*regexp.Regexp) bool {
	if len(whitelist) > 0 {
		// Whitelist mode: block unless UA matches at least one whitelist pattern
		for _, re := range whitelist {
			if re.MatchString(ua) {
				return false
			}
		}
		return true
	}
	if len(blacklist) > 0 {
		// Blacklist mode: block if UA matches any blacklist pattern
		for _, re := range blacklist {
			if re.MatchString(ua) {
				return true
			}
		}
	}
	return false
}
