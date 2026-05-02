package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func runMsg(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("msg: usage: coda-lite msg <to> <body> | --file <path> | --stdin")
	}
	to := args[0]
	rest := args[1:]

	var body string
	switch rest[0] {
	case "--file":
		if len(rest) != 2 {
			return fmt.Errorf("msg: --file requires a path")
		}
		b, err := os.ReadFile(rest[1])
		if err != nil {
			return fmt.Errorf("read %s: %w", rest[1], err)
		}
		body = string(b)
	case "--stdin":
		if len(rest) != 1 {
			return fmt.Errorf("msg: --stdin takes no further arguments")
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		body = string(b)
	default:
		body = strings.Join(rest, " ")
	}

	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("msg: body is empty")
	}

	dir, err := mustAgentDir(to)
	if err != nil {
		return err
	}

	from := os.Getenv("USER")
	if from == "" {
		from = "unknown"
	}

	inbox := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return fmt.Errorf("ensure inbox: %w", err)
	}

	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	filename := fmt.Sprintf("%s-from-%s.md", ts, sanitize(from))
	path := filepath.Join(inbox, filename)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("Sent. %s\n", path)
	return nil
}

func runInbox(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("inbox: usage: coda-lite inbox <name>")
	}
	name := args[0]
	dir, err := mustAgentDir(name)
	if err != nil {
		return err
	}
	inbox := filepath.Join(dir, "inbox")

	entries, err := os.ReadDir(inbox)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Inbox is empty.")
			return nil
		}
		return fmt.Errorf("read %s: %w", inbox, err)
	}
	files := []os.DirEntry{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files = append(files, e)
	}
	if len(files) == 0 {
		fmt.Println("Inbox is empty.")
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		fmt.Println(filepath.Join(inbox, f.Name()))
	}
	return nil
}

func runRead(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("read: usage: coda-lite read <inbox-file>")
	}
	src, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	parent := filepath.Dir(src)
	if filepath.Base(parent) != "inbox" {
		return fmt.Errorf("not in an inbox: %s", src)
	}
	agentRoot := filepath.Dir(parent)

	base := filepath.Base(src)
	sender := senderFromName(base)
	if sender == "" {
		sender = "unknown"
	}

	dst := filepath.Join(agentRoot, "outbox", "from-"+sender, "read", base)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("ensure outbox dir: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move %s -> %s: %w", src, dst, err)
	}
	fmt.Printf("Marked read. %s\n", dst)
	return nil
}

func senderFromName(filename string) string {
	idx := strings.Index(filename, "-from-")
	if idx < 0 {
		return ""
	}
	tail := filename[idx+len("-from-"):]
	tail = strings.TrimSuffix(tail, ".md")
	return tail
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
