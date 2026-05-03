package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func AgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, "agents"), nil
}

func AgentDir(name string) (string, error) {
	root, err := AgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func AgentExists(name string) (bool, string, error) {
	dir, err := AgentDir(name)
	if err != nil {
		return false, "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, dir, nil
		}
		return false, dir, err
	}
	if !info.IsDir() {
		return false, dir, fmt.Errorf("%s exists but is not a directory", dir)
	}
	return true, dir, nil
}

func MustAgentDir(name string) (string, error) {
	exists, dir, err := AgentExists(name)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("agent %q does not exist (looked at %s)", name, dir)
	}
	return dir, nil
}

func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid name %q (no dot-files)", name)
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == ' ' {
			return fmt.Errorf("invalid name %q (no slashes or spaces)", name)
		}
	}
	return nil
}
