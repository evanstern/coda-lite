package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func NewAgent(dir, name string) error {
	for _, sub := range []string{"memory", "wiki", "wiki/decisions", "wiki/entities", "inbox", "outbox", "features"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", sub, err)
		}
	}

	files := map[string]string{
		"AGENTS.md":       agentsTemplate(name),
		"wiki/index.md":   wikiIndexTemplate(name),
		".coda-lite-meta": metaTemplate(name),
		"opencode.json":   opencodeTemplate(),
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	memPath := filepath.Join(dir, "memory", today+".md")
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		body := fmt.Sprintf("# Memory — %s\n\nAgent %q created.\n", today, name)
		if err := os.WriteFile(memPath, []byte(body), 0o644); err != nil {
			return fmt.Errorf("write initial memory: %w", err)
		}
	}

	return nil
}

func ImplementTemplate(agent, slug string) string {
	return fmt.Sprintf(`# Feature: %s

Spawned by: %s

## Brief

(The spawning agent should rewrite this section with the actual brief.)

## Context

- Worktree: this directory
- Branch: feature/%s

## Done when

- [ ] (define done conditions)

## Notes

`, slug, agent, slug)
}

func metaTemplate(name string) string {
	return strings.TrimSpace(fmt.Sprintf(`
name = "%s"
harness = "opencode"
created = "%s"
`, name, time.Now().UTC().Format(time.RFC3339))) + "\n"
}

func opencodeTemplate() string {
	return `{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "mcp": {
    "coda-lite": {
      "type": "local",
      "enabled": true,
      "command": ["coda-lite", "mcp", "serve"]
    }
  }
}
`
}

func wikiIndexTemplate(name string) string {
	return fmt.Sprintf(`# Wiki — %s

Curated knowledge base for %s. Pages live under %swiki/%s.

## Conventions

- Each page is a markdown file under wiki/<topic>.md or wiki/<category>/<topic>.md
- Link with [[page-name]] (no extension, no path)
- Each page starts with a short description and ends with related links

## Pages

(empty — add pages as you learn things worth keeping)
`, name, name, "`", "`")
}

func agentsTemplate(name string) string {
	tmpl := `# AGENTS.md — NAME

You are NAME. This file is your boot instructions.

## Identity

(Edit this section to define the agent's name, role, voice, and values.
This is the personality layer. It survives across sessions.)

## On boot, do this

1. **Read recent memory.** Read ` + "`memory/$(date -u +%Y-%m-%d).md`" + ` if
   it exists, plus the most recent prior day's file. These are
   append-only narrative entries written as things happen.

2. **Skim the wiki.** Read ` + "`wiki/index.md`" + `. Note pages relevant to
   your current work. The wiki is curated knowledge — prefer it over
   digging through raw memory.

3. **Check your inbox.** Run ` + "`coda-lite inbox NAME`" + `. Each path is
   an unread message. Read what needs reading. After handling a
   message, mark it read with ` + "`coda-lite read <path>`" + `.

## While running

- **Write to memory.** As things happen, append to
  ` + "`memory/$(date -u +%Y-%m-%d).md`" + `. Append-only, narrative style.
  Don't batch — write inline as decisions are made or things
  surprise you.
- **Curate the wiki.** When durable facts surface (decisions,
  entities, patterns, lessons), write or update a wiki page. The
  wiki is the layer that ages well; memory is the layer that
  captures the moment.
- **Poll the inbox at natural breakpoints.** No one will notify you
  of new messages. Run ` + "`coda-lite inbox NAME`" + ` when you finish a
  task or pause.
- **Cite memory and wiki when you use them.** If a wiki page or
  memory entry informed your answer, name it. Makes reasoning
  auditable.

## Sending messages

- ` + "`coda-lite msg <to> \"<body>\"`" + ` — sends a message; recipient
  sees it on their next inbox check.
- ` + "`coda-lite msg <to> --stdin`" + ` — body from stdin, useful for long
  multi-line messages.
- No types, no acks. The body is markdown. Use a leading
  ` + "`## Type: brief`" + ` line if you want a convention.

## How to be

(Add personality, values, voice notes here. These are read on every
boot and shape how the agent behaves.)
`
	return strings.ReplaceAll(tmpl, "NAME", name)
}
