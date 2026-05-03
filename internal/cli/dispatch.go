package cli

import (
	"context"
	"fmt"

	codalitemcp "github.com/evanstern/coda-lite/internal/mcp"
)

const Version = "0.2.0"

func Dispatch(args []string) error {
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "agent":
		return dispatchAgent(rest)
	case "msg":
		return runMsg(rest)
	case "inbox":
		return runInbox(rest)
	case "read":
		return runRead(rest)
	case "feature":
		return dispatchFeature(rest)
	case "mcp":
		return dispatchMCP(rest)
	case "version", "--version", "-v":
		fmt.Println(Version)
		return nil
	case "help", "--help", "-h":
		return nil
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func dispatchAgent(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("agent: missing subcommand (new|import|ls|spawn|attach|stop|rm)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "new":
		return runAgentNew(rest)
	case "import":
		return runAgentImport(rest)
	case "ls":
		return runAgentLs(rest)
	case "spawn":
		return runAgentSpawn(rest)
	case "attach":
		return runAgentAttach(rest)
	case "stop":
		return runAgentStop(rest)
	case "rm":
		return runAgentRm(rest)
	default:
		return fmt.Errorf("agent: unknown subcommand: %s", sub)
	}
}

func dispatchMCP(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mcp: missing subcommand (serve)")
	}
	switch args[0] {
	case "serve":
		return codalitemcp.Serve(context.Background())
	default:
		return fmt.Errorf("mcp: unknown subcommand: %s", args[0])
	}
}

func dispatchFeature(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("feature: missing subcommand (start|ls|finish)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "start":
		return runFeatureStart(rest)
	case "ls":
		return runFeatureLs(rest)
	case "finish":
		return runFeatureFinish(rest)
	default:
		return fmt.Errorf("feature: unknown subcommand: %s", sub)
	}
}
