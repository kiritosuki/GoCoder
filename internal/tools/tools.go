// Package tools 提供 Agent 工具系统：注册、定义、调度
package tools

import (
	"fmt"
	"log"
	"time"

	"github.com/kiritosuki/gocoder/internal/types"
)

// Registry 工具注册表
type Registry struct {
	tools map[string]*types.RegisteredTool
	order []string // 保持注册顺序
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*types.RegisteredTool),
		order: []string{},
	}
}

// Register 注册一个工具
func (r *Registry) Register(rt *types.RegisteredTool) {
	name := rt.Definition.Function.Name
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = rt
}

// Get 获取单个工具
func (r *Registry) Get(name string) (*types.RegisteredTool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List 返回所有工具定义（用于发送给 LLM）
func (r *Registry) List() []types.ToolDefinition {
	defs := make([]types.ToolDefinition, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.tools[name]; ok {
			defs = append(defs, t.Definition)
		}
	}
	return defs
}

// ListNames 返回所有工具名称
func (r *Registry) ListNames() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// RequiresApproval 检查工具是否需要审批
func (r *Registry) RequiresApproval(name string) bool {
	if t, ok := r.tools[name]; ok {
		return t.RequiresApproval
	}
	return true // 未知工具默认需要审批
}

// RiskLevel 返回工具的风险等级
func (r *Registry) RiskLevel(name string) string {
	if t, ok := r.tools[name]; ok {
		return t.RiskLevel
	}
	return "unknown"
}

// Execute 执行指定工具
func (r *Registry) Execute(name string, ctx types.ToolContext, input map[string]any) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("未知工具: %s", name)
	}
	start := time.Now()
	output, err := t.Executor(ctx, input)
	elapsed := time.Since(start)
	log.Printf("[tool] %s 执行完成，耗时 %v", name, elapsed)
	return output, err
}

// BuildToolDef 创建工具定义的辅助函数
func BuildToolDef(name, desc string, params map[string]any) types.ToolDefinition {
	return types.ToolDefinition{
		Type: "function",
		Function: types.FunctionSchema{
			Name:        name,
			Description: desc,
			Parameters:  params,
		},
	}
}

// ──── 通用 JSON Schema 构造器 ────

// ObjectSchema 构造一个 object 类型的 JSON Schema
func ObjectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// StringProp 构造一个 string 属性
func StringProp(desc string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": desc,
	}
}

// IntProp 构造一个 integer 属性
func IntProp(desc string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": desc,
	}
}

// BoolProp 构造一个 boolean 属性
func BoolProp(desc string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": desc,
	}
}

// ──── 内置工具注册 ────

// RegisterBuiltins 注册所有内置工具到注册表
func RegisterBuiltins(r *Registry, cfg BuiltinConfig) {
	registerFileTools(r, cfg)
	registerShellTool(r, cfg)
}

// BuiltinConfig 内置工具需要的配置
type BuiltinConfig struct {
	WorkDir       string
	ToolResultDir string
}
