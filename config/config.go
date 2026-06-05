package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kiritosuki/gocoder/internal/types"
	"go.yaml.in/yaml/v3"
)

const (
	configRelPath       = ".gocoder/config.yaml"
	backupConfigRelPath = ".gocoder/.backup-config.yaml"
	toolResultsDir      = ".gocoder/tool_results"
	sessionsDir         = ".gocoder/sessions"
	skillsDir           = ".gocoder/skills"
)

//go:embed config.yaml
var embeddedConfig []byte

// LoadAppConfig 加载应用配置，不存在时用内嵌默认配置创建
func LoadAppConfig() (types.AppConfig, error) {
	fullPath, err := fullPath(configRelPath)
	if err != nil {
		return types.AppConfig{}, fmt.Errorf("获取配置路径失败: %w", err)
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return createDefaultConfig(configRelPath)
	}
	return loadExistingConfig(configRelPath)
}

// GetModelConfig 获取当前模型配置
func GetModelConfig(cfg types.AppConfig) (types.ModelConfig, error) {
	if cfg.Model.Name == "" {
		return types.ModelConfig{}, fmt.Errorf("请在配置文件中设置 model 字段")
	}
	return cfg.Model, nil
}

// SaveAppConfig 保存应用配置
func SaveAppConfig(cfg types.AppConfig) error {
	fullPath, _ := fullPath(configRelPath)
	return writeConfigFile(cfg, fullPath)
}

// DataDir 返回 GoCoder 数据目录的完整路径
func DataDir() (string, error) {
	return fullPath(".gocoder")
}

// ToolResultsDir 返回工具结果落盘目录
func ToolResultsDir() (string, error) {
	return fullPath(toolResultsDir)
}

// SessionsDir 返回会话存储目录
func SessionsDir() (string, error) {
	return fullPath(sessionsDir)
}

// SkillsDir 返回 skills 目录
func SkillsDir() (string, error) {
	return fullPath(skillsDir)
}

// EnsureDirs 确保必要的目录存在
func EnsureDirs() error {
	for _, d := range []string{toolResultsDir, sessionsDir, skillsDir} {
		fp, err := fullPath(d)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(fp, 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", fp, err)
		}
	}
	return nil
}

// ──── 内部辅助 ────

func fullPath(relPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}
	return filepath.Join(home, relPath), nil
}

func createDefaultConfig(relPath string) (types.AppConfig, error) {
	full, _ := fullPath(relPath)
	cfg := types.AppConfig{}
	if err := yaml.Unmarshal(embeddedConfig, &cfg); err != nil {
		return cfg, fmt.Errorf("解析内嵌配置失败: %w", err)
	}
	// 允许环境变量覆盖默认模型
	if override := os.Getenv("GOCODER_MODEL"); override != "" {
		cfg.Model.Name = override
	}
	return cfg, writeConfigFile(cfg, full)
}

func writeConfigFile(cfg types.AppConfig, fullPath string) error {
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return saveBackup(cfg)
}

func loadExistingConfig(relPath string) (types.AppConfig, error) {
	full, _ := fullPath(relPath)
	cfg := types.AppConfig{}
	data, err := os.ReadFile(full)
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件失败: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件失败: %w", err)
	}
	return cfg, nil
}

func saveBackup(cfg types.AppConfig) error {
	full, err := fullPath(backupConfigRelPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, _ := yaml.Marshal(cfg)
	return os.WriteFile(full, data, 0644)
}

// ──── 公开工具函数 ────

// PrintConfigError 格式化输出配置错误
func PrintConfigError(err error) {
	fmt.Printf("\n  ⚠ 配置加载失败\n  %v\n\n", err)
}

// ResetToDefault 重置配置为默认值
func ResetToDefault() error {
	_, err := createDefaultConfig(configRelPath)
	return err
}

// RevertToBackup 恢复到备份配置
func RevertToBackup() error {
	full, _ := fullPath(configRelPath)
	os.Remove(full)
	cfg, err := loadExistingConfig(backupConfigRelPath)
	if err != nil {
		return err
	}
	return writeConfigFile(cfg, full)
}
