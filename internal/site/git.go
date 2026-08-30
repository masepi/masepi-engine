package site

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runGit(ctx context.Context, binary, directory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.CombinedOutput()
	message := strings.TrimSpace(string(output))
	if err != nil {
		if message == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(arguments, " "), message, err)
	}
	return message, nil
}

func gitRevision(ctx context.Context, binary, directory string) (string, error) {
	return runGit(ctx, binary, directory, "rev-parse", "HEAD")
}

func shortRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) > 8 {
		return revision[:8]
	}
	if revision == "" {
		return "unknown"
	}
	return revision
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
