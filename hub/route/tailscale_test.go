package route

import (
	"io"
	"strings"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
)

func TestTailscaleRoutesRequireAuthButWebShellIsPublic(t *testing.T) {
	handler := NewHandler(false, "secret", "", Cors{})

	for _, path := range []string{"/tailscale", "/tailscale/logs"} {
		req, err := http.NewRequest("GET", path, nil)
		if err != nil {
			t.Fatalf("NewRequest(%s): %v", path, err)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", path, rec.Code)
		}

		req, err = http.NewRequest("GET", path, nil)
		if err != nil {
			t.Fatalf("NewRequest(%s): %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer secret")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s authed status = %d, want 200; body=%s", path, rec.Code, rec.Body.String())
		}
	}

	req, err := http.NewRequest("GET", "/tailscale/web", nil)
	if err != nil {
		t.Fatalf("NewRequest(/tailscale/web): %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/tailscale/web status = %d, want 200", rec.Code)
	}
}

func TestTailscaleRoutesAllowEmptySecret(t *testing.T) {
	handler := NewHandler(false, "", "", Cors{})

	for _, path := range []string{"/tailscale", "/tailscale/logs"} {
		req, err := http.NewRequest("GET", path, nil)
		if err != nil {
			t.Fatalf("NewRequest(%s): %v", path, err)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestTailscaleWebShellHasSecurityHeadersAndNoRuntimeData(t *testing.T) {
	handler := NewHandler(false, "secret", "", Cors{})
	req, err := http.NewRequest("GET", "/tailscale/web", nil)
	if err != nil {
		t.Fatalf("NewRequest(/tailscale/web): %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html; charset=utf-8") {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}

	body := rec.Body.String()
	for _, forbidden := range []string{"nodekey:", "authURL", "stateDir", "tailscaled.state"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("web shell contains forbidden runtime token %q", forbidden)
		}
	}
	for _, required := range []string{"api('/tailscale')", "api('/tailscale/logs')", "sessionStorage"} {
		if !strings.Contains(body, required) {
			t.Fatalf("web shell missing %q", required)
		}
	}
}

func TestTailscaleRoutesRemainStableAcrossHandlers(t *testing.T) {
	for i := 0; i < 3; i++ {
		handler := NewHandler(false, "secret", "", Cors{})
		req, err := http.NewRequest("GET", "/tailscale/web", nil)
		if err != nil {
			t.Fatalf("NewRequest(/tailscale/web): %v", err)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("handler %d web status = %d, want 200", i, rec.Code)
		}

		req, err = http.NewRequest("GET", "/tailscale/logs", nil)
		if err != nil {
			t.Fatalf("NewRequest(/tailscale/logs): %v", err)
		}
		req.Header.Set("Authorization", "Bearer secret")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b, _ := io.ReadAll(rec.Body)
			t.Fatalf("handler %d logs status = %d, want 200; body=%s", i, rec.Code, b)
		}
	}
}
