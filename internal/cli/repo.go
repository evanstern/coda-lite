package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/evanstern/coda-lite/internal/repo"
)

func runRepoBareInit(args []string) error {
	yes := false
	pos := []string{}
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			yes = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 1 {
		return fmt.Errorf("repo bare-init: usage: coda-lite repo bare-init <path> [--yes]")
	}
	if strings.TrimSpace(pos[0]) == "" {
		return fmt.Errorf("repo bare-init: <path> is empty")
	}
	_, err := repo.BareInit(repo.BareInitOptions{
		Path: pos[0],
		Yes:  yes,
		In:   os.Stdin,
		Out:  os.Stdout,
	})
	return err
}
