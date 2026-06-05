package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kiritosuki/gocoder/internal/types"
)

const (
	defaultShellTimeout  = 120 * time.Second
	maxShellOutput       = 10000 // 字符
	shellOutputDiskLimit = 5000  // 超过此长度触发落盘
)

func registerShellTool(r *Registry, cfg BuiltinConfig) {
	r.Register(&types.RegisteredTool{
		Definition: BuildToolDef("run_command", "在终端中执行 shell 命令。命令需要用户审批。输出过长时会自动落盘。",
			ObjectSchema(map[string]any{
				"command":         StringProp("要执行的 shell 命令"),
				"working_dir":     StringProp("工作目录（可选，默认为项目目录）"),
				"timeout_seconds": IntProp("超时时间秒数（可选，默认 120）"),
				"background":      BoolProp("是否后台执行（可选，默认 false）"),
			}, []string{"command"})),
		Executor:         makeRunCommand(cfg),
		RequiresApproval: true,
		RiskLevel:        "shell",
	})
}

func makeRunCommand(cfg BuiltinConfig) types.ToolExecutor {
	return func(ctx types.ToolContext, input map[string]any) (string, error) {
		command := getString(input, "command")
		if command == "" {
			return "", fmt.Errorf("缺少 command 参数")
		}

		// 工作目录
		workDir := getString(input, "working_dir")
		if workDir == "" {
			workDir = cfg.WorkDir
		}
		resolvedWorkDir, err := resolveWorkspacePath(cfg.WorkDir, workDir)
		if err != nil {
			return "", err
		}

		// 超时
		timeout := getInt(input, "timeout_seconds")
		if timeout <= 0 {
			timeout = int(defaultShellTimeout.Seconds())
		}

		// 后台执行
		background := false
		if v, ok := input["background"]; ok {
			if b, ok := v.(bool); ok {
				background = b
			}
		}

		if background {
			return runBackground(command, resolvedWorkDir)
		}
		return runForeground(command, resolvedWorkDir, time.Duration(timeout)*time.Second)
	}
}

func runForeground(command, workDir string, timeout time.Duration) (string, error) {
	execCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	cmd.Dir = workDir

	// 环境变量白名单
	cmd.Env = filteredEnv()

	output, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("命令超时（>%v）: %s", timeout, command)
		} else {
			return "", fmt.Errorf("命令执行失败: %w", err)
		}
	}

	result := string(output)
	truncated := false
	diskPath := ""

	// 输出过长处理
	if utf8.RuneCountInString(result) > maxShellOutput {
		result = result[:maxShellOutput]
		truncated = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("$ %s\n", command))

	if truncated {
		// 完整输出落盘
		diskPath = saveToDisk("shell", string(output))
		// 上下文保留预览
		preview := result[:2000]
		tail := ""
		if len(result) > 2500 {
			tail = result[len(result)-500:]
		}
		sb.WriteString(fmt.Sprintf("%s\n...[truncated, 完整输出: %s]...\n%s", preview, diskPath, tail))
	} else {
		sb.WriteString(result)
	}

	if exitCode != 0 {
		sb.WriteString(fmt.Sprintf("\n(exit code: %d)", exitCode))
	}

	return sb.String(), nil
}

func runBackground(command, workDir string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workDir
	cmd.Env = filteredEnv()

	// 分离进程
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("后台启动失败: %w", err)
	}

	// 不等待，立即返回
	go func() {
		cmd.Wait()
	}()

	return fmt.Sprintf("后台任务已启动 (PID %d): %s\n工作目录: %s", cmd.Process.Pid, command, workDir), nil
}

func filteredEnv() []string {
	// 白名单环境变量
	whitelist := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "SHELL": true,
		"LANG": true, "TERM": true, "PWD": true, "OLDPWD": true,
		"GOPATH": true, "GOROOT": true, "GOBIN": true,
		"JAVA_HOME": true, "NODE_PATH": true, "PYTHONPATH": true,
		"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true,
		"DISPLAY": true,
	}
	var env []string
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 && whitelist[parts[0]] {
			env = append(env, e)
		}
	}
	return env
}

// saveToDisk 将内容保存到落盘目录
func saveToDisk(category, content string) string {
	dir := filepath.Join(os.Getenv("HOME"), ".gocoder", "tool_results")
	os.MkdirAll(dir, 0755)

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.txt", timestamp, category)
	fullPath := filepath.Join(dir, filename)

	os.WriteFile(fullPath, []byte(content), 0644)
	return fullPath
}
