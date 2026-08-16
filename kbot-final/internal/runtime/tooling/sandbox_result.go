package tooling

import "context"

// SandboxResult 是 Runner 返回给运行时审计链路的结构化执行结果。
type SandboxResult struct {
	ExecutionID     string
	ContainerName   string
	Language        string
	Stdout          string
	Stderr          string
	ExitCode        int
	DurationMS      int64
	TimedOut        bool
	OutputTruncated bool
}

type DetailedSandboxRunner interface {
	RunDetailed(ctx context.Context, language, code string) (SandboxResult, error)
}

type sandboxObserverKey struct{}
type SandboxObserver func(SandboxResult, error) error

func WithSandboxObserver(ctx context.Context, observer SandboxObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, sandboxObserverKey{}, observer)
}

func notifySandboxObserver(ctx context.Context, result SandboxResult, runErr error) error {
	observer, _ := ctx.Value(sandboxObserverKey{}).(SandboxObserver)
	if observer == nil {
		return nil
	}
	return observer(result, runErr)
}
