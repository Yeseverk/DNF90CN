package scripthost

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrScriptNameRequired 表示脚本名称为空。
	ErrScriptNameRequired = errors.New("script name is required")

	// ErrScriptSourceEmpty 表示脚本源码为空。
	ErrScriptSourceEmpty = errors.New("script source is empty")

	// ErrScriptHostRequired 表示脚本宿主或程序实现缺失。
	ErrScriptHostRequired = errors.New("script host is required")
)

// Script 描述一份可编译的脚本源码。
type Script struct {
	Name     string            `json:"name"`
	Language string            `json:"language"`
	Source   []byte            `json:"source,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Request 是脚本执行时传入宿主的最小请求。
type Request struct {
	Script   string            `json:"script"`
	Function string            `json:"function,omitempty"`
	Input    []byte            `json:"input,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Result 是脚本执行后的返回结果。
type Result struct {
	Output   []byte            `json:"output,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Program 必须可并发 Execute；脚本运行时的全局状态隔离由具体 Host 负责。
type Program interface {
	Execute(context.Context, Request) (Result, error)
}

// Host 编译脚本并返回可执行程序。
// Compile 应只做编译与绑定，不应在持久化热路径里启动长生命周期 goroutine。
type Host interface {
	Compile(context.Context, Script) (Program, error)
}

// ProgramFunc 把普通函数适配为 Program。
type ProgramFunc func(context.Context, Request) (Result, error)

// Execute 执行适配后的脚本函数。
func (f ProgramFunc) Execute(ctx context.Context, request Request) (Result, error) {
	if f == nil {
		return Result{}, ErrScriptHostRequired
	}
	return f(ctx, request)
}

// HostFunc 把普通函数适配为 Host。
type HostFunc func(context.Context, Script) (Program, error)

// Compile 编译脚本并返回可执行程序。
func (f HostFunc) Compile(ctx context.Context, script Script) (Program, error) {
	if f == nil {
		return nil, ErrScriptHostRequired
	}
	return f(ctx, script)
}

// ValidateScript 校验脚本名称和源码是否满足编译前置条件。
func ValidateScript(script Script) error {
	if strings.TrimSpace(script.Name) == "" {
		return ErrScriptNameRequired
	}
	if len(script.Source) == 0 {
		return ErrScriptSourceEmpty
	}
	return nil
}

// CompileAndExecute 编译脚本并立即执行一次。
func CompileAndExecute(ctx context.Context, host Host, script Script, request Request) (Result, error) {
	if host == nil {
		return Result{}, ErrScriptHostRequired
	}
	if err := ValidateScript(script); err != nil {
		return Result{}, err
	}
	program, err := host.Compile(ctx, script)
	if err != nil {
		return Result{}, err
	}
	if program == nil {
		return Result{}, ErrScriptHostRequired
	}
	if strings.TrimSpace(request.Script) == "" {
		request.Script = script.Name
	}
	return program.Execute(ctx, request)
}
