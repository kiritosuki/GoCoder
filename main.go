package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kiritosuki/gocoder/config"
	"github.com/kiritosuki/gocoder/internal/tui"
	"github.com/kiritosuki/gocoder/internal/types"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "gocoder",
		Short: "GoCoder - 终端 AI Coding Agent",
		Long: `GoCoder 是一个轻量级终端编码助手。
兼容 OpenAI API 规范，通过配置文件管理模型。

示例:
  gocoder              # 启动交互式 TUI
  gocoder config show  # 查看配置
  gocoder version      # 显示版本`,
		Run: runGoCoder,
	}

	configCmd = &cobra.Command{
		Use:   "config",
		Short: "管理 GoCoder 配置",
		Run: func(cmd *cobra.Command, args []string) {
			runConfig(args)
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "显示版本信息",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("GoCoder v1.0.0")
		},
	}
)

func init() {
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runGoCoder(_ *cobra.Command, _ []string) {
	appConfig, err := config.LoadAppConfig()
	if err != nil {
		config.PrintConfigError(err)
		os.Exit(1)
	}

	modelConfig, err := config.GetModelConfig(appConfig)
	if err != nil {
		config.PrintConfigError(err)
		os.Exit(1)
	}

	// 获取 API Key
	apiKey := os.Getenv(modelConfig.AuthEnvVar)
	if apiKey == "" {
		printAPIKeyNotSet(modelConfig)
		os.Exit(1)
	}
	modelConfig.AuthEnvVar = apiKey // 注入实际 key

	// 设置工作目录
	if appConfig.Preferences.ProjectDir == "." || appConfig.Preferences.ProjectDir == "" {
		cwd, _ := os.Getwd()
		appConfig.Preferences.ProjectDir = cwd
	}

	if err := config.EnsureDirs(); err != nil {
		fmt.Printf("创建数据目录失败: %v\n", err)
		os.Exit(1)
	}

	if err := tui.Run(appConfig, modelConfig); err != nil {
		fmt.Printf("运行失败: %v\n", err)
		os.Exit(1)
	}
}

func runConfig(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "reset":
			if err := config.ResetToDefault(); err != nil {
				fmt.Printf("重置失败: %v\n", err)
			} else {
				fmt.Println("✓ 配置已重置")
			}
			return
		case "revert":
			if err := config.RevertToBackup(); err != nil {
				fmt.Printf("恢复失败: %v\n", err)
			} else {
				fmt.Println("✓ 配置已恢复")
			}
			return
		case "show":
			cfg, err := config.LoadAppConfig()
			if err != nil {
				fmt.Printf("加载失败: %v\n", err)
				return
			}
			model, _ := config.GetModelConfig(cfg)
			fmt.Printf("模型: %s | %d tokens\n", model.Name, model.MaxTokens)
			fmt.Printf("工具轮数: %d\n", cfg.Preferences.MaxToolRounds)
			fmt.Printf("Compact 阈值: %.0f%%\n", cfg.Preferences.CompactThreshold*100)
			
			fmt.Printf("MCP Servers: %d 个\n", len(cfg.MCPServers))
			return
		}
	}
	fmt.Println("用法: gocoder config [reset|revert|show]")
}

func printAPIKeyNotSet(mc types.ModelConfig) {
	fmt.Printf("\n  ⚠ API Key 未设置\n\n")
	fmt.Printf("  模型 '%s' 需要设置环境变量: %s\n\n", mc.Name, mc.AuthEnvVar)
	fmt.Printf("  示例:\n")
	fmt.Printf("    export %s=\"your-api-key\"\n\n", mc.AuthEnvVar)
	fmt.Printf("  获取 API Key:\n")
	if strings.Contains(mc.Endpoint, "openai.com") {
		fmt.Printf("    https://platform.openai.com/api-keys\n")
	} else if strings.Contains(mc.Endpoint, "deepseek.com") {
		fmt.Printf("    https://platform.deepseek.com/api-keys\n")
	}
	fmt.Println()
}
