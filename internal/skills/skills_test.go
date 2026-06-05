package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".gocoder", "skills")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go-release.md"), []byte(`---
name: go-release
description: Go 项目发布流程。当用户提到"发布"、"release"、"发版"时使用。
keywords: ["发布", "release", "发版"]
---

# Go Release Process

1. 运行测试
2. 创建 tag
`), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	mgr := NewManager()
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}

	skills := mgr.List()
	if len(skills) == 0 {
		t.Fatal("未加载到任何 skill")
	}

	t.Logf("加载 %d 个 skill", len(skills))

	// 测试按名称获取
	s := mgr.Get("go-release")
	if s == nil {
		t.Fatal("Get(go-release) 返回 nil")
	}
	if s.Name != "go-release" {
		t.Errorf("skill name = %q, want go-release", s.Name)
	}
	if s.Content == "" {
		t.Error("skill content 为空")
	}
	t.Logf("skill go-release 加载成功: %d bytes", len(s.Content))

	// 测试关键词匹配
	matched := mgr.Match("帮我发布一个新版本")
	if matched == nil {
		t.Error("Match('帮我发布一个新版本') 返回 nil, 期望匹配 go-release")
	} else if matched.Name != "go-release" {
		t.Errorf("匹配到 %s, 期望 go-release", matched.Name)
	}

	// 测试激活
	activated, err := mgr.Activate("go-release")
	if err != nil {
		t.Fatalf("Activate 失败: %v", err)
	}
	if activated == nil {
		t.Fatal("Activate 返回 nil")
	}
	if mgr.ActiveSkill() == nil {
		t.Error("ActiveSkill 返回 nil")
	}

	// 测试 system prompt 构建
	prompt := mgr.BuildSystemPrompt()
	if prompt == "" {
		t.Error("BuildSystemPrompt 返回空")
	}
	t.Logf("system prompt: %d chars", len(prompt))

	// 测试取消激活
	mgr.Deactivate()
	if mgr.ActiveSkill() != nil {
		t.Error("Deactivate 后 ActiveSkill 应为 nil")
	}

	// 测试不匹配
	notMatched := mgr.Match("写一个排序算法")
	if notMatched != nil {
		t.Errorf("Match('写一个排序算法') 不应匹配, 但匹配到 %s", notMatched.Name)
	}

	t.Log("Skills 测试全部通过")
}

func TestLoadFromDirDedupKeywordsAndPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release.md"), []byte(`---
name: Release Flow
description: 当用户提到"shipit"时使用。
keywords: ["publish-now"]
---

# Release

Run focused checks.
`), 0644); err != nil {
		t.Fatalf("write release skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z_override.md"), []byte(`---
name: release-flow
description: override description
keywords: ["override-key"]
---

# Override
`), 0644); err != nil {
		t.Fatalf("write override skill: %v", err)
	}

	mgr := NewManager()
	if err := mgr.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if names := mgr.ListNames(); len(names) != 1 || names[0] != "release-flow" {
		t.Fatalf("unexpected names: %v", names)
	}

	if matched := mgr.Match("please use override-key for this"); matched == nil || matched.Name != "release-flow" {
		t.Fatalf("keyword match failed: %+v", matched)
	}
	if _, err := mgr.Activate("Release Flow"); err != nil {
		t.Fatalf("activate normalized name: %v", err)
	}
	prompt := mgr.BuildSystemPrompt()
	if !strings.Contains(prompt, "# Override") {
		t.Fatalf("prompt did not use override content: %s", prompt)
	}
}

func TestLoadAllOnlyUsesHomeGocoderSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldCwd)

	projectSkillDir := filepath.Join(cwd, "skills")
	if err := os.MkdirAll(projectSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectSkillDir, "project.md"), []byte(`---
name: project-only
description: project skill
---
# Project
`), 0644); err != nil {
		t.Fatal(err)
	}

	homeSkillDir := filepath.Join(home, ".gocoder", "skills")
	if err := os.MkdirAll(homeSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeSkillDir, "home.md"), []byte(`---
name: home-only
description: home skill
---
# Home
`), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if mgr.Get("home-only") == nil {
		t.Fatal("expected ~/.gocoder skill to be loaded")
	}
	if mgr.Get("project-only") != nil {
		t.Fatal("project skills should not be loaded by LoadAll")
	}
}
