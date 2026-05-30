package proxy

import (
	"regexp"
	"testing"
)
func TestShouldLogPath(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		want  bool
		category string
	}{
		// Deny exact tests
		{
			name:  "deny_exact_admin",
			path:  "/admin",
			want:  false,
			category: "deny exact",
		},

		// Deny prefix tests
		{
			name:  "deny_prefix_admin_users",
			path:  "/admin/users",
			want:  false,
			category: "deny prefix",
		},
		{
			name:  "deny_prefix_api_v1_admin",
			path:  "/api/v1/admin/users",
			want:  false,
			category: "deny prefix",
		},
		{
			name:  "deny_prefix_api_v1_auth",
			path:  "/api/v1/auth/login",
			want:  false,
			category: "deny prefix",
		},
		{
			name:  "deny_prefix_api_v1_user",
			path:  "/api/v1/user/profile",
			want:  false,
			category: "deny prefix",
		},
		{
			name:  "deny_prefix_api_v1_keys",
			path:  "/api/v1/keys",
			want:  false,
			category: "deny prefix",
		},
		{
			name:  "deny_prefix_api_v1_payment",
			path:  "/api/v1/payment/webhook/test",
			want:  false,
			category: "deny prefix",
		},
		{
			name:  "deny_prefix_api_v1_pages",
			path:  "/api/v1/pages/home",
			want:  false,
			category: "deny prefix",
		},

		// Allow exact tests
		{
			name:  "allow_exact_responses",
			path:  "/responses",
			want:  true,
			category: "allow exact",
		},
		{
			name:  "allow_exact_chat_completions",
			path:  "/chat/completions",
			want:  true,
			category: "allow exact",
		},
		{
			name:  "allow_exact_embeddings",
			path:  "/embeddings",
			want:  true,
			category: "allow exact",
		},

		// Allow prefix tests
		{
			name:  "allow_prefix_v1",
			path:  "/v1/chat/completions",
			want:  true,
			category: "allow prefix",
		},
		{
			name:  "allow_prefix_v1beta",
			path:  "/v1beta/models",
			want:  true,
			category: "allow prefix",
		},
		{
			name:  "allow_prefix_images",
			path:  "/images/generations",
			want:  true,
			category: "allow prefix",
		},
		{
			name:  "allow_prefix_backend_api_codex",
			path:  "/backend-api/codex/execute",
			want:  true,
			category: "allow prefix",
		},
		{
			name:  "allow_prefix_antigravity",
			path:  "/antigravity/v1/chat/completions",
			want:  true,
			category: "allow prefix",
		},

		// Unknown/default tests
		{
			name:  "unknown_path_health",
			path:  "/health",
			want:  false,
			category: "unknown path",
		},
		{
			name:  "unknown_path_v1_admin_users",
			path:  "/v1/admin/users",
			want:  true, // /v1/admin/users starts with /v1/ which is allowed
			category: "unknown path",
		},
	}

	// Run tests organized by category
	t.Run("deny exact", func(t *testing.T) {
		for _, tt := range tests {
			if tt.category != "deny exact" {
				continue
			}
			t.Run(tt.name, func(t *testing.T) {
				if got := shouldLogPath(tt.path); got != tt.want {
					t.Errorf("shouldLogPath(%q) = %v, want %v", tt.path, got, tt.want)
				}
			})
		}
	})

	t.Run("deny prefix", func(t *testing.T) {
		for _, tt := range tests {
			if tt.category != "deny prefix" {
				continue
			}
			t.Run(tt.name, func(t *testing.T) {
				if got := shouldLogPath(tt.path); got != tt.want {
					t.Errorf("shouldLogPath(%q) = %v, want %v", tt.path, got, tt.want)
				}
			})
		}
	})

	t.Run("allow exact", func(t *testing.T) {
		for _, tt := range tests {
			if tt.category != "allow exact" {
				continue
			}
			t.Run(tt.name, func(t *testing.T) {
				if got := shouldLogPath(tt.path); got != tt.want {
					t.Errorf("shouldLogPath(%q) = %v, want %v", tt.path, got, tt.want)
				}
			})
		}
	})

	t.Run("allow prefix", func(t *testing.T) {
		for _, tt := range tests {
			if tt.category != "allow prefix" {
				continue
			}
			t.Run(tt.name, func(t *testing.T) {
				if got := shouldLogPath(tt.path); got != tt.want {
					t.Errorf("shouldLogPath(%q) = %v, want %v", tt.path, got, tt.want)
				}
			})
		}
	})

	t.Run("unknown path", func(t *testing.T) {
		for _, tt := range tests {
			if tt.category != "unknown path" {
				continue
			}
			t.Run(tt.name, func(t *testing.T) {
				if got := shouldLogPath(tt.path); got != tt.want {
					t.Errorf("shouldLogPath(%q) = %v, want %v", tt.path, got, tt.want)
				}
			})
		}
	})
}

func TestShouldBlockUA(t *testing.T) {
	tests := []struct {
		name      string
		ua        string
		whitelist []string // regex patterns to compile
		blacklist []string // regex patterns to compile
		want      bool
	}{
		{
			name: "no_lists_allows_all",
			ua:   "curl/7.0",
			want: false,
		},
		{
			name:      "whitelist_match_allows",
			ua:        "python-requests/2.0",
			whitelist: []string{"python-requests"},
			want:      false,
		},
		{
			name:      "whitelist_no_match_blocks",
			ua:        "curl/7.0",
			whitelist: []string{"python-requests"},
			want:      true,
		},
		{
			name:      "whitelist_regex_match",
			ua:        "Mozilla/5.0 (compatible; Googlebot/2.1)",
			whitelist: []string{"(?i)googlebot"},
			want:      false,
		},
		{
			name:      "whitelist_regex_no_match",
			ua:        "Mozilla/5.0 (Windows NT 10.0)",
			whitelist: []string{"(?i)googlebot"},
			want:      true,
		},
		{
			name:      "blacklist_match_blocks",
			ua:        "curl/7.0",
			blacklist: []string{"curl"},
			want:      true,
		},
		{
			name:      "blacklist_no_match_allows",
			ua:        "python-requests/2.0",
			blacklist: []string{"curl"},
			want:      false,
		},
		{
			name:      "blacklist_regex_match",
			ua:        "Mozilla/5.0 (compatible; Bingbot/2.0)",
			blacklist: []string{"(?i)bot"},
			want:      true,
		},
		{
			name:      "blacklist_regex_no_match",
			ua:        "Mozilla/5.0 (compatible)",
			blacklist: []string{"(?i)bot"},
			want:      false,
		},
		{
			name:      "whitelist_takes_precedence_over_blacklist",
			ua:        "curl/7.0",
			whitelist: []string{"python"},
			blacklist: []string{"curl"},
			want:      true, // whitelist active, curl doesn't match whitelist
		},
		{
			name:      "whitelist_empty_ua_blocks",
			ua:        "",
			whitelist: []string{"python"},
			want:      true,
		},
		{
			name:      "blacklist_empty_ua_allows",
			ua:        "",
			blacklist: []string{"curl"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile pattern strings to regexps
			whitelist := make([]*regexp.Regexp, len(tt.whitelist))
			for i, p := range tt.whitelist {
				re, err := regexp.Compile(p)
				if err != nil {
					t.Fatalf("invalid whitelist pattern %q: %v", p, err)
				}
				whitelist[i] = re
			}
			blacklist := make([]*regexp.Regexp, len(tt.blacklist))
			for i, p := range tt.blacklist {
				re, err := regexp.Compile(p)
				if err != nil {
					t.Fatalf("invalid blacklist pattern %q: %v", p, err)
				}
				blacklist[i] = re
			}

			if got := shouldBlockUA(tt.ua, whitelist, blacklist); got != tt.want {
				t.Errorf("shouldBlockUA(%q) = %v, want %v", tt.ua, got, tt.want)
			}
		})
	}
}
