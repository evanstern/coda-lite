package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

func agentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, "agents"), nil
}

func agentDir(name string) (string, error) {
	root, err := agentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func agentExists(name string) (bool, string, error) {
	dir, err := agentDir(name)
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

func mustAgentDir(name string) (string, error) {
	exists, dir, err := agentExists(name)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("agent %q does not exist (looked at %s)", name, dir)
	}
	return dir, nil
}
