package tmux

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func SessionExists(name string) (bool, error) {
	cmd := exec.Command("tmux", "has-session", "-t", "="+name)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func NewSession(name, dir, command string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", dir}
	if command != "" {
		args = append(args, command)
	}
	cmd := exec.Command("tmux", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", "="+name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func Attach(name string) error {
	binary, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscall.Exec(binary, []string{"tmux", "attach", "-t", "=" + name}, os.Environ())
}

func NewWindow(session, name, dir string) error {
	cmd := exec.Command("tmux", "new-window", "-t", "="+session, "-n", name, "-c", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
