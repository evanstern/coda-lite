package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanstern/coda-lite/internal/scaffold"
	"github.com/evanstern/coda-lite/internal/tmux"
)

func runAgentNew(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("agent new: usage: coda-lite agent new <name>")
	}
	name := args[0]
	if err := validateName(name); err != nil {
		return err
	}

	exists, dir, err := agentExists(name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("agent %q already exists at %s", name, dir)
	}

	if err := scaffold.NewAgent(dir, name); err != nil {
		return fmt.Errorf("scaffold: %w", err)
	}
	fmt.Printf("Created agent %q at %s\n", name, dir)
	fmt.Printf("Edit %s/AGENTS.md to set personality and boot instructions.\n", dir)
	fmt.Printf("Spawn with: coda-lite agent spawn %s\n", name)
	return nil
}

func runAgentImport(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("agent import: usage: coda-lite agent import <existing-path> [name]")
	}
	src, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	name := filepath.Base(src)
	if len(args) == 2 {
		name = args[1]
	}
	if err := validateName(name); err != nil {
		return err
	}

	root, err := agentsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}

	dst := filepath.Join(root, name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("agent %q already exists at %s", name, dst)
	}

	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
	}

	for _, sub := range []string{"inbox", "outbox"} {
		if err := os.MkdirAll(filepath.Join(src, sub), 0o755); err != nil {
			return fmt.Errorf("ensure %s/%s: %w", src, sub, err)
		}
	}

	fmt.Printf("Imported %q from %s\n", name, src)
	fmt.Printf("Symlinked at %s\n", dst)
	if _, err := os.Stat(filepath.Join(src, "AGENTS.md")); os.IsNotExist(err) {
		fmt.Printf("Note: no AGENTS.md found in source. Add one to set boot instructions.\n")
	}
	return nil
}

func runAgentLs(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("agent ls: takes no arguments")
	}
	root, err := agentsDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No agents yet. Create one with: coda-lite agent new <name>")
			return nil
		}
		return fmt.Errorf("read %s: %w", root, err)
	}

	type row struct {
		name    string
		running bool
		path    string
	}
	rows := []row{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir, _ := agentDir(e.Name())
		running, _ := tmux.SessionExists(e.Name())
		rows = append(rows, row{name: e.Name(), running: running, path: dir})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	if len(rows) == 0 {
		fmt.Println("No agents yet. Create one with: coda-lite agent new <name>")
		return nil
	}
	for _, r := range rows {
		status := "stopped"
		if r.running {
			status = "running"
		}
		fmt.Printf("%-20s  %-8s  %s\n", r.name, status, r.path)
	}
	return nil
}

func runAgentSpawn(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("agent spawn: usage: coda-lite agent spawn <name>")
	}
	name := args[0]
	dir, err := mustAgentDir(name)
	if err != nil {
		return err
	}
	if running, _ := tmux.SessionExists(name); running {
		return fmt.Errorf("agent %q is already running (tmux session %q exists)", name, name)
	}

	harness := scaffold.HarnessFor(dir)
	cmd, err := harness.SpawnCommand()
	if err != nil {
		return err
	}
	if err := tmux.NewSession(name, dir, cmd); err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	fmt.Printf("Spawned agent %q (tmux session: %s)\n", name, name)
	fmt.Printf("Attach with: coda-lite agent attach %s\n", name)
	return nil
}

func runAgentAttach(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("agent attach: usage: coda-lite agent attach <name>")
	}
	name := args[0]
	if _, err := mustAgentDir(name); err != nil {
		return err
	}
	if running, _ := tmux.SessionExists(name); !running {
		return fmt.Errorf("agent %q is not running. Spawn with: coda-lite agent spawn %s", name, name)
	}
	return tmux.Attach(name)
}

func runAgentStop(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("agent stop: usage: coda-lite agent stop <name>")
	}
	name := args[0]
	if _, err := mustAgentDir(name); err != nil {
		return err
	}
	if running, _ := tmux.SessionExists(name); !running {
		fmt.Printf("Agent %q is already stopped\n", name)
		return nil
	}
	if err := tmux.KillSession(name); err != nil {
		return fmt.Errorf("kill tmux session: %w", err)
	}
	fmt.Printf("Stopped agent %q\n", name)
	return nil
}

func runAgentRm(args []string) error {
	force := false
	pos := []string{}
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 1 {
		return fmt.Errorf("agent rm: usage: coda-lite agent rm <name> [--force]")
	}
	name := pos[0]
	dir, err := mustAgentDir(name)
	if err != nil {
		return err
	}
	if running, _ := tmux.SessionExists(name); running {
		return fmt.Errorf("agent %q is running. Stop it first: coda-lite agent stop %s", name, name)
	}
	if !force {
		return fmt.Errorf("refusing to remove %s without --force\n  this deletes memory, wiki, inbox, outbox", dir)
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(dir); err != nil {
			return fmt.Errorf("unlink %s: %w", dir, err)
		}
		fmt.Printf("Removed symlink %s (target preserved)\n", dir)
		return nil
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	fmt.Printf("Removed agent %q (%s)\n", name, dir)
	return nil
}
