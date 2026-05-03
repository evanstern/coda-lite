// Command coda-lite is a single-user AI dev tool: long-running
// personality-driven agents over tmux + opencode + markdown.
//
// The filesystem is the bus. Agent directory is identity + memory +
// queue. Tmux session is "is this agent currently running." Markdown
// is everything.
package main

import (
	"fmt"
	"os"

	"github.com/evanstern/coda-lite/internal/cli"
)

const usage = `coda-lite — single-user AI dev tool

Usage:
  coda-lite agent new <name>            scaffold a new agent
  coda-lite agent import <path> [name]  link an existing agent dir into ~/agents/
  coda-lite agent ls                    list agents and their tmux session status
  coda-lite agent spawn <name>          start the agent's tmux session (running opencode)
  coda-lite agent attach <name>         attach to the agent's tmux session
  coda-lite agent stop <name>           kill the agent's tmux session (memory survives)
  coda-lite agent rm <name> [--force]   remove the agent's directory

  coda-lite msg <to> <body>             send a message (writes to recipient's inbox)
  coda-lite msg <to> --file <path>      send a message body from a file
  coda-lite msg <to> --stdin            send a message body from stdin
  coda-lite inbox <name>                list unread messages for the named agent
  coda-lite read <inbox-file>           mark a message read (move to outbox/)

  coda-lite feature start <agent> <slug> --repo <path>   create worktree + spawn session
  coda-lite feature ls                                    list active feature sessions
  coda-lite feature finish <slug>                         end a feature session

  coda-lite mcp serve                   run an MCP server on stdio (for opencode.json)

  coda-lite version                     print version

The filesystem is the bus. Recipients poll their inbox; senders don't notify.
See ~/agents/<name>/AGENTS.md for boot instructions.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	if err := cli.Dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "coda-lite: %v\n", err)
		os.Exit(1)
	}
}
