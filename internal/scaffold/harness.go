package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Harness struct {
	Name string
}

func HarnessFor(agentDir string) Harness {
	meta := filepath.Join(agentDir, ".coda-lite-meta")
	b, err := os.ReadFile(meta)
	if err != nil {
		return Harness{Name: "opencode"}
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "harness") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"`)
			if val != "" {
				return Harness{Name: val}
			}
		}
	}
	return Harness{Name: "opencode"}
}

func (h Harness) SpawnCommand() (string, error) {
	switch h.Name {
	case "opencode":
		return "opencode", nil
	case "shell":
		return "", nil
	default:
		return "", fmt.Errorf("unknown harness %q (supported: opencode, shell)", h.Name)
	}
}
