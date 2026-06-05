package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveWorkspacePath(workDir, path string) (string, error) {
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	if strings.TrimSpace(path) == "" {
		path = "."
	}

	root, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("解析工作目录失败: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			root = filepath.Clean(root)
		} else {
			return "", fmt.Errorf("解析工作目录符号链接失败: %w", err)
		}
	}

	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %w", err)
	}

	existing := target
	for {
		if _, err := os.Stat(existing); err == nil {
			if resolved, err := filepath.EvalSymlinks(existing); err == nil {
				target = filepath.Join(resolved, strings.TrimPrefix(target, existing))
			}
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}

	rel, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil {
		return "", fmt.Errorf("计算相对路径失败: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("路径越界: %s 不在 workspace %s 内", path, root)
	}
	return filepath.Clean(target), nil
}
