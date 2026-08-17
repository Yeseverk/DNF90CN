package runner

import (
	"context"
	"flag"
	"log"
	"os"

	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/i18n"
	runtimekit "longheng.io/server/internal/runtime/bootstrap"
)

type Factory = appkit.Builder

func Run(serviceName, defaultConfigPath string, factory Factory) {
	if err := appkit.RunManifestFromFlags(serviceName, defaultConfigPath, runtimekit.Manifest(factory, i18n.ConfigureApp)); err != nil {
		log.Fatal(err)
	}
}

func RunContext(ctx context.Context, serviceName, defaultConfigPath string, factory Factory) {
	if err := appkit.RunManifestFromFlagSet(
		ctx,
		flag.CommandLine,
		os.Args[1:],
		serviceName,
		defaultConfigPath,
		runtimekit.Manifest(factory, i18n.ConfigureApp),
	); err != nil {
		log.Fatal(err)
	}
}
