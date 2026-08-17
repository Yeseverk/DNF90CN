package main

import (
	"context"
	"errors"
)

type launcherAction uint8

const (
	launcherActionLogin launcherAction = iota
	launcherActionRegister
)

type launcherStage uint8

const (
	launcherStageIdle launcherStage = iota
	launcherStageEnvironment
	launcherStageServer
	launcherStageAccount
	launcherStageClient
	launcherStageComplete
)

type launcherCommandRunner func(
	context.Context,
	[]string,
	string,
) (string, error)

type launcherStageReporter func(launcherStage, string)

func runLauncherWorkflow(
	ctx context.Context,
	action launcherAction,
	username string,
	password string,
	validateClient func() error,
	run launcherCommandRunner,
	report launcherStageReporter,
) (string, error) {
	if run == nil {
		return "", errors.New("launcher command runner is required")
	}
	reportStage(report, launcherStageEnvironment, "正在检查客户端配置…")
	if validateClient != nil {
		if err := validateClient(); err != nil {
			return "", err
		}
	}

	reportStage(report, launcherStageServer, "正在启动并检查本地服务…")
	output, err := run(ctx, []string{"start"}, "")
	if err != nil {
		return output, err
	}

	if action == launcherActionRegister {
		reportStage(report, launcherStageAccount, "正在注册账号…")
		output, err = run(
			ctx,
			[]string{
				"account",
				"register",
				"--username",
				username,
				"--password-stdin",
				"--keep-database",
			},
			password,
		)
		if err != nil {
			return output, err
		}
		reportStage(report, launcherStageClient, "账号已注册，正在启动客户端…")
	} else {
		reportStage(report, launcherStageAccount, "正在验证账号并启动客户端…")
	}

	output, err = run(
		ctx,
		[]string{
			"launch-client",
			"--multi-instance",
			"--username",
			username,
			"--password-stdin",
		},
		password,
	)
	if err != nil {
		return output, err
	}
	reportStage(report, launcherStageClient, "客户端已启动。")
	reportStage(report, launcherStageComplete, "登录完成。")
	return output, nil
}

func reportStage(
	report launcherStageReporter,
	stage launcherStage,
	message string,
) {
	if report != nil {
		report(stage, message)
	}
}
