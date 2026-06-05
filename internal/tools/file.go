package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kiritosuki/gocoder/internal/types"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func registerFileTools(r *Registry, cfg BuiltinConfig) {
	// ──── read_file ────
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("read_file", "读取文件内容，返回带行号的文本。支持指定行范围。",
			ObjectSchema(map[string]any{
				"path":   StringProp("文件路径（相对于项目目录）"),
				"offset": IntProp("起始行号（1-based，可选）"),
				"limit":  IntProp("读取行数（可选，默认全部）"),
			}, []string{"path"})),
		Executor:         makeReadFile(cfg),
		RequiresApproval: false,
		RiskLevel:        "readonly",
	})

	// ──── write_file ────
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("write_file", "创建或覆盖文件。会先生成 diff 供审查，需要用户确认。",
			ObjectSchema(map[string]any{
				"path":    StringProp("文件路径（相对于项目目录）"),
				"content": StringProp("要写入的完整文件内容"),
			}, []string{"path", "content"})),
		Executor:         makeWriteFile(cfg),
		RequiresApproval: true,
		RiskLevel:        "write",
	})

	// ──── edit_file ────
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("edit_file", "精确字符串替换编辑文件。查找 old_string 并替换为 new_string。",
			ObjectSchema(map[string]any{
				"path":       StringProp("文件路径（相对于项目目录）"),
				"old_string": StringProp("要被替换的原始文本（必须精确匹配）"),
				"new_string": StringProp("替换后的新文本"),
			}, []string{"path", "old_string", "new_string"})),
		Executor:         makeEditFile(cfg),
		RequiresApproval: true,
		RiskLevel:        "write",
	})

	// ──── list_directory ────
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("list_directory", "列出目录内容。支持递归深度控制。",
			ObjectSchema(map[string]any{
				"path":  StringProp("目录路径（相对于项目目录，默认 '.'）"),
				"depth": IntProp("递归深度（默认 2，设为 1 则只列一级）"),
			}, []string{"path"})),
		Executor:         makeListDirectory(cfg),
		RequiresApproval: false,
		RiskLevel:        "readonly",
	})

	// ──── grep ────
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("grep", "在文件中搜索正则表达式模式，返回匹配的文件名、行号和内容。",
			ObjectSchema(map[string]any{
				"pattern": StringProp("正则表达式搜索模式"),
				"path":    StringProp("搜索目录（默认 '.'）"),
				"include": StringProp("文件过滤 glob（如 '*.go'，可选）"),
			}, []string{"pattern"})),
		Executor:         makeGrep(cfg),
		RequiresApproval: false,
		RiskLevel:        "readonly",
	})

	// ──── read_tool_result ────
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("read_tool_result",
			"读取之前因过大而落盘的工具输出文件。当工具返回结果被截断时，使用此工具读取完整内容。",
			ObjectSchema(map[string]any{
				"path": StringProp("工具输出的文件路径（从截断提示中获取）"),
			}, []string{"path"})),
		Executor:         makeReadToolResult(cfg),
		RequiresApproval: false,
		RiskLevel:        "readonly",
	})
}

// ──── read_file 实现 ────

func makeReadFile(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		path := getString(input, "path")
		if path == "" {
			return "", fmt.Errorf("缺少 path 参数")
		}
		fullPath, err := resolveWorkspacePath(cfg.WorkDir, path)
		if err != nil {
			return "", err
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("读取文件失败: %w", err)
		}

		offset := getInt(input, "offset")
		limit := getInt(input, "limit")

		lines := strings.Split(string(data), "\n")
		if offset > 0 {
			if offset > len(lines) {
				offset = len(lines)
			}
			lines = lines[offset-1:]
		}
		if limit > 0 && limit < len(lines) {
			lines = lines[:limit]
		}

		// 带行号输出
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("// %s (%d lines)\n", path, len(lines)))
		startLine := 1
		if offset > 0 {
			startLine = offset
		}
		for i, line := range lines {
			sb.WriteString(fmt.Sprintf("%6d| %s\n", startLine+i, line))
		}
		return sb.String(), nil
	}
}

// ──── write_file 实现 ────

func makeWriteFile(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		path := getString(input, "path")
		content := getString(input, "content")
		if path == "" {
			return "", fmt.Errorf("缺少 path 参数")
		}
		fullPath, err := resolveWorkspacePath(cfg.WorkDir, path)
		if err != nil {
			return "", err
		}

		// 读取旧内容（如果存在）
		oldContent := ""
		if data, err := os.ReadFile(fullPath); err == nil {
			oldContent = string(data)
		}

		// 生成 diff
		diff := generateDiff(path, oldContent, content)

		// 确保目录存在
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("创建目录失败: %w", err)
		}

		// 写入文件
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("写入文件失败: %w", err)
		}

		result := fmt.Sprintf("文件 %s 已写入 (%d bytes)", path, len(content))
		if diff != "" {
			result = diff + "\n\n" + result
		}
		return result, nil
	}
}

// ──── edit_file 实现 ────

func makeEditFile(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		path := getString(input, "path")
		oldStr := getString(input, "old_string")
		newStr := getString(input, "new_string")
		if path == "" || oldStr == "" {
			return "", fmt.Errorf("缺少 path 或 old_string 参数")
		}
		fullPath, err := resolveWorkspacePath(cfg.WorkDir, path)
		if err != nil {
			return "", err
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("读取文件失败: %w", err)
		}
		content := string(data)

		// 检查 old_string 是否唯一
		count := strings.Count(content, oldStr)
		if count == 0 {
			return "", fmt.Errorf("未找到匹配的 old_string，请确认内容精确匹配")
		}
		if count > 1 {
			return "", fmt.Errorf("old_string 匹配到 %d 处，请提供更精确的上下文以确保唯一匹配", count)
		}

		newContent := strings.Replace(content, oldStr, newStr, 1)

		// 生成 diff
		diff := generateDiff(path, content, newContent)

		// 写入
		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("写入文件失败: %w", err)
		}

		result := fmt.Sprintf("文件 %s 已修改", path)
		if diff != "" {
			result = diff + "\n\n" + result
		}
		return result, nil
	}
}

// ──── list_directory 实现 ────

func makeListDirectory(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		path := getString(input, "path")
		if path == "" {
			path = "."
		}
		depth := getInt(input, "depth")
		if depth == 0 {
			depth = 2
		}
		fullPath, err := resolveWorkspacePath(cfg.WorkDir, path)
		if err != nil {
			return "", err
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("// %s (depth=%d)\n", path, depth))
		err = walkDir(fullPath, "", depth, &sb)
		if err != nil {
			return "", err
		}
		return sb.String(), nil
	}
}

func walkDir(basePath, prefix string, depth int, sb *strings.Builder) error {
	if depth <= 0 {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(basePath, prefix))
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		// 跳过隐藏文件和常见忽略目录
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		relPath := filepath.Join(prefix, name)
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("  📁 %s/\n", relPath))
			walkDir(basePath, relPath, depth-1, sb)
		} else {
			info, _ := e.Info()
			size := ""
			if info != nil {
				s := info.Size()
				if s < 1024 {
					size = fmt.Sprintf(" (%dB)", s)
				} else if s < 1024*1024 {
					size = fmt.Sprintf(" (%dKB)", s/1024)
				} else {
					size = fmt.Sprintf(" (%dMB)", s/(1024*1024))
				}
			}
			sb.WriteString(fmt.Sprintf("  📄 %s%s\n", relPath, size))
		}
	}
	return nil
}

// ──── grep 实现 ────

func makeGrep(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		pattern := getString(input, "pattern")
		if pattern == "" {
			return "", fmt.Errorf("缺少 pattern 参数")
		}
		path := getString(input, "path")
		if path == "" {
			path = "."
		}
		include := getString(input, "include")

		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("正则表达式无效: %w", err)
		}

		fullPath, err := resolveWorkspacePath(cfg.WorkDir, path)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("// grep: %s in %s\n", pattern, path))

		count := 0
		maxMatches := 50 // 限制返回数量

		filepath.Walk(fullPath, func(fp string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			// 跳过隐藏/大文件/二进制
			name := info.Name()
			if strings.HasPrefix(name, ".") || info.Size() > 500*1024 {
				return nil
			}
			if include != "" {
				matched, _ := filepath.Match(include, name)
				if !matched {
					return nil
				}
			}
			// 检查是否是文本文件（简单判断）
			data, err := os.ReadFile(fp)
			if err != nil {
				return nil
			}
			if !isTextFile(data) {
				return nil
			}

			lines := strings.Split(string(data), "\n")
			rel, _ := filepath.Rel(cfg.WorkDir, fp)
			for i, line := range lines {
				if count >= maxMatches {
					return filepath.SkipAll
				}
				if re.MatchString(line) {
					sb.WriteString(fmt.Sprintf("%s:%d: %s\n", rel, i+1, strings.TrimSpace(line)))
					count++
				}
			}
			return nil
		})

		if count >= maxMatches {
			sb.WriteString(fmt.Sprintf("\n... (结果截断，共找到 %d+ 处匹配)", maxMatches))
		} else {
			sb.WriteString(fmt.Sprintf("\n// 共 %d 处匹配", count))
		}
		return sb.String(), nil
	}
}

// ──── 辅助函数 ────

func getString(input map[string]any, key string) string {
	if v, ok := input[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(input map[string]any, key string) int {
	if v, ok := input[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func generateDiff(path, oldContent, newContent string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(oldContent, newContent, true)
	diffs = dmp.DiffCleanupSemantic(diffs)

	if len(diffs) == 1 && diffs[0].Type == diffmatchpatch.DiffEqual {
		return "" // 无变化
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", path, path))

	// 按行分组，生成 unified diff 风格输出
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	_ = oldLines
	_ = newLines

	// 使用 DiffPrettyText 获取可读的 diff
	pretty := dmp.DiffPrettyText(diffs)
	sb.WriteString(pretty)

	return sb.String()
}

// PreviewWriteDiff builds the same diff that write_file would return, without
// modifying the file. It is used by the permission prompt.
func PreviewWriteDiff(workDir, path, newContent string) (string, error) {
	fullPath, err := resolveWorkspacePath(workDir, path)
	if err != nil {
		return "", err
	}
	oldContent := ""
	if data, err := os.ReadFile(fullPath); err == nil {
		oldContent = string(data)
	}
	return generateDiff(path, oldContent, newContent), nil
}

// PreviewEditDiff builds the diff for an edit_file request, enforcing the same
// uniqueness rule as the executor.
func PreviewEditDiff(workDir, path, oldString, newString string) (string, error) {
	fullPath, err := resolveWorkspacePath(workDir, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	content := string(data)
	count := strings.Count(content, oldString)
	if count == 0 {
		return "", fmt.Errorf("未找到匹配的 old_string")
	}
	if count > 1 {
		return "", fmt.Errorf("old_string 匹配到 %d 处", count)
	}
	return generateDiff(path, content, strings.Replace(content, oldString, newString, 1)), nil
}

func isTextFile(data []byte) bool {
	// 简单判断：检查前 512 字节是否包含 null 字节
	n := 512
	if len(data) < n {
		n = len(data)
	}
	for _, b := range data[:n] {
		if b == 0 {
			return false
		}
	}
	return true
}

// ──── read_tool_result 实现 ────

func makeReadToolResult(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		path := getString(input, "path")
		if path == "" {
			return "", fmt.Errorf("缺少 path 参数")
		}
		fullPath, err := resolveWorkspacePath(cfg.ToolResultDir, path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("读取工具输出文件失败: %w", err)
		}
		return string(data), nil
	}
}
