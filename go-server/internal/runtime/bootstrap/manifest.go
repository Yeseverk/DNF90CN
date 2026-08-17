package bootstrap

import (
	"longheng.io/server/internal/platform/app"
)

func Manifest(builder app.Builder, configurers ...app.EnvConfigurer) app.ServiceManifest {
	return app.ServiceManifest{
		Configure:  append([]app.EnvConfigurer(nil), configurers...),
		Components: builder,
	}
}
