package route

import (
	"context"
	_ "embed"
	"time"

	"github.com/metacubex/chi"
	"github.com/metacubex/chi/render"
	"github.com/metacubex/http"
	"github.com/metacubex/mihomo/component/tsnet"
)

const tailscaleStatusTimeout = 3 * time.Second

//go:embed assets/tailscale.html
var tailscaleWebHTML []byte

func init() {
	Register(func(r chi.Router) {
		r.Get("/tailscale", getTailscaleStatus)
		r.Get("/tailscale/logs", getTailscaleLogs)
	})
	RegisterPublic(func(r chi.Router) {
		r.Get("/tailscale/web", getTailscaleWeb)
	})
}

func getTailscaleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), tailscaleStatusTimeout)
	defer cancel()
	render.JSON(w, r, tsnet.Status(ctx))
}

func getTailscaleLogs(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, tsnet.Logs())
}

func getTailscaleWeb(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(tailscaleWebHTML)
}
