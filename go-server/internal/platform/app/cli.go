package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
)

// EmitCompatDeprecation 在旧版 cmd/<service> 二进制启动时向 stderr 打一行警告。
// 生产构建应切到 cmd/server/<service>；CI smoke 如需静默，可设置
// 设置 LONGHENG_SUPPRESS_DEPRECATION=1。
func EmitCompatDeprecation(serviceName string) {
	if os.Getenv("LONGHENG_SUPPRESS_DEPRECATION") == "1" {
		return
	}
	fmt.Fprintf(os.Stderr,
		"[deprecated] cmd/%s is a compat entry; production builds must use cmd/server/%s. "+
			"See cmd/README.md. Set LONGHENG_SUPPRESS_DEPRECATION=1 to silence.\n",
		serviceName, serviceName)
}

// RunFromFlags 使用进程默认命令行参数启动基于 Builder 的服务。
func RunFromFlags(serviceName, defaultConfigPath string, builder Builder) error {
	return RunFromFlagSet(context.Background(), flag.CommandLine, os.Args[1:], serviceName, defaultConfigPath, builder)
}

// RunManifestFromFlags 使用进程默认命令行参数启动基于 ServiceManifest 的服务。
func RunManifestFromFlags(serviceName, defaultConfigPath string, manifest ServiceManifest) error {
	return RunManifestFromFlagSet(context.Background(), flag.CommandLine, os.Args[1:], serviceName, defaultConfigPath, manifest)
}

// RunFromFlagSet 使用指定 FlagSet 和参数启动基于 Builder 的服务，便于测试或自定义入口复用。
func RunFromFlagSet(ctx context.Context, fs *flag.FlagSet, args []string, serviceName, defaultConfigPath string, builder Builder) error {
	return RunManifestFromFlagSet(ctx, fs, args, serviceName, defaultConfigPath, ServiceManifest{Components: builder})
}

// RunManifestFromFlagSet 解析配置路径并按 ServiceManifest 完成服务装配和运行。
func RunManifestFromFlagSet(ctx context.Context, fs *flag.FlagSet, args []string, serviceName, defaultConfigPath string, manifest ServiceManifest) error {
	if fs == nil {
		return errors.New("flag set is nil")
	}
	configPath := defaultConfigPath
	if existing := fs.Lookup("config"); existing == nil {
		fs.StringVar(&configPath, "config", defaultConfigPath, "path to config file")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if existing := fs.Lookup("config"); existing != nil {
		configPath = existing.Value.String()
	}
	return RunWithManifest(ctx, serviceName, configPath, manifest)
}
