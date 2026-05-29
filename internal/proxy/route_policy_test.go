package proxy

import "testing"

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
