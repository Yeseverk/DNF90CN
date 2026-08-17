package main

import (
	"context"

	"longheng.io/server/cmd/server/internal/runner"
	dnf90app "longheng.io/server/internal/app/dnf90"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.RunContext(ctx, dnf90app.ServiceName, dnf90app.DefaultConfigPath, dnf90app.ComponentsWithShutdown(cancel))
}
