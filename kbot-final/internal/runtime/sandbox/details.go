package sandbox

import (
	"context"

	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

func (s *Sandbox) RunDetailed(ctx context.Context, language, code string) (tooling.SandboxResult, error) {
	result, err := s.Execute(ctx, language, code)
	return detailedResult(language, result), err
}

func (c *Client) RunDetailed(ctx context.Context, language, code string) (tooling.SandboxResult, error) {
	result, err := c.Execute(ctx, language, code)
	return detailedResult(language, result), err
}

func detailedResult(language string, result Result) tooling.SandboxResult {
	return tooling.SandboxResult{
		ExecutionID: result.ExecutionID, ContainerName: result.ContainerName, Language: language,
		Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode,
		DurationMS: result.DurationMS, TimedOut: result.TimedOut, OutputTruncated: result.OutputTruncated,
	}
}
