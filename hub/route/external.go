package route

import "github.com/metacubex/chi"

type externalRouter func(r chi.Router)

var (
	externalRouters       = make([]externalRouter, 0)
	publicExternalRouters = make([]externalRouter, 0)
)

func Register(route ...externalRouter) {
	externalRouters = append(externalRouters, route...)
}

func RegisterPublic(route ...externalRouter) {
	publicExternalRouters = append(publicExternalRouters, route...)
}

func addExternalRouters(r chi.Router) {
	if len(externalRouters) == 0 {
		return
	}

	for _, caller := range externalRouters {
		caller(r)
	}
}

func addPublicExternalRouters(r chi.Router) {
	if len(publicExternalRouters) == 0 {
		return
	}

	for _, caller := range publicExternalRouters {
		caller(r)
	}
}
