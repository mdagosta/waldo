package index

import (
	"errors"
	"os/exec"
	"strings"
)

type Identity struct {
	Remote string `json:"remote,omitempty"`
	Commit string `json:"commit,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

// Identify records the Git identity of an index checkout. A valid index is
// still readable outside Git, in which case the empty identity honestly says
// that no revision pin is available.
func Identify(root string) Identity {
	commit, commitErr := gitOutput(root, "rev-parse", "HEAD")
	if commitErr != nil {
		return Identity{}
	}
	remote, _ := gitOutput(root, "config", "--get", "remote.origin.url")
	status, statusErr := gitOutput(root, "status", "--porcelain", "--untracked-files=normal")
	return Identity{
		Remote: remote,
		Commit: commit,
		Dirty:  statusErr != nil || status != "",
	}
}

func gitOutput(root string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", exit
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
