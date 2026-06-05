// Package permission 提供工具执行的权限审批框架
package permission

import (
	"regexp"
	"strings"

	"github.com/kiritosuki/gocoder/internal/types"
)

// Runner 权限审批运行器
type Runner struct {
	alwaysAllow map[string]bool                     // 本次会话始终允许的工具
	alwaysDeny  map[string]bool                     // 本次会话始终拒绝的工具
	decisions   map[string]types.PermissionDecision // call_id → decision
}

// NewRunner 创建权限审批器
func NewRunner() *Runner {
	return &Runner{
		alwaysAllow: map[string]bool{
			// 只读工具默认自动允许
			"read_file":        true,
			"grep":             true,
			"list_directory":   true,
			"read_tool_result": true,
		},
		alwaysDeny: map[string]bool{},
		decisions:  map[string]types.PermissionDecision{},
	}
}

// Check 检查工具是否需要审批
// 返回 PermAllow/PermDeny/PermAllowAll 等
func (r *Runner) Check(toolName string) types.PermissionDecision {
	if r.alwaysDeny[toolName] {
		return types.PermDeny
	}
	if r.alwaysAllow[toolName] {
		return types.PermAllow
	}
	// 不在白名单也不在黑名单 → 需要交互审批
	return "" // 空字符串表示需要询问
}

// AllowAlways 将工具加入永久允许列表
func (r *Runner) AllowAlways(toolName string) {
	r.alwaysAllow[toolName] = true
	delete(r.alwaysDeny, toolName)
}

// DenyAlways 将工具加入永久拒绝列表
func (r *Runner) DenyAlways(toolName string) {
	r.alwaysDeny[toolName] = true
	delete(r.alwaysAllow, toolName)
}

// RecordDecision 记录特定 call 的审批决策
func (r *Runner) RecordDecision(callID string, decision types.PermissionDecision) {
	r.decisions[callID] = decision
}

// GetDecision 获取特定 call 的决策
func (r *Runner) GetDecision(callID string) (types.PermissionDecision, bool) {
	d, ok := r.decisions[callID]
	return d, ok
}

// Reset 重置所有自定义规则（保留默认白名单）
func (r *Runner) Reset() {
	r.alwaysAllow = map[string]bool{
		"read_file":        true,
		"grep":             true,
		"list_directory":   true,
		"read_tool_result": true,
	}
	r.alwaysDeny = map[string]bool{}
	r.decisions = map[string]types.PermissionDecision{}
}

// AlwaysAllowList 返回当前永久允许的工具列表
func (r *Runner) AlwaysAllowList() []string {
	var names []string
	for name := range r.alwaysAllow {
		names = append(names, name)
	}
	return names
}

// AlwaysDenyList 返回当前永久拒绝的工具列表
func (r *Runner) AlwaysDenyList() []string {
	var names []string
	for name := range r.alwaysDeny {
		names = append(names, name)
	}
	return names
}

// AnalyzeShellCommand returns compact risk labels for a shell command.
// It is intentionally conservative: suspicious syntax is surfaced to the user
// for approval instead of being silently treated as safe.
func AnalyzeShellCommand(command string) []string {
	lower := strings.ToLower(command)
	checks := []struct {
		label string
		re    *regexp.Regexp
	}{
		{"destructive", regexp.MustCompile(`(^|[;&|()\s])(rm|rmdir|shred)\s`)},
		{"privileged", regexp.MustCompile(`(^|[;&|()\s])sudo\s`)},
		{"git-history", regexp.MustCompile(`git\s+(reset|clean|checkout|restore)\b`)},
		{"network", regexp.MustCompile(`(^|[;&|()\s])(curl|wget|scp|ssh|npm|npx|pip|go\s+get|go\s+install)\b`)},
		{"permission-change", regexp.MustCompile(`(^|[;&|()\s])(chmod|chown)\s`)},
		{"file-write", regexp.MustCompile(`(^|[^>])>{1,2}[^>]`)},
		{"pipe-to-shell", regexp.MustCompile(`(curl|wget)[^|]*\|\s*(sh|bash|zsh)`)},
		{"background", regexp.MustCompile(`&\s*$`)},
	}

	var risks []string
	for _, check := range checks {
		if check.re.MatchString(lower) {
			risks = append(risks, check.label)
		}
	}
	return risks
}
