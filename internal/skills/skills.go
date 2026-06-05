// Package skills loads and matches SKILL.md-style process knowledge.
// Skills are prompt instructions, not executable tools.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/kiritosuki/gocoder/config"
	"go.yaml.in/yaml/v3"
)

var quotedKeywordRE = regexp.MustCompile(`"([^"]+)"|'([^']+)'|“([^”]+)”|‘([^’]+)’`)

type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Content     string
	FilePath    string
	Keywords    []string `yaml:"keywords,omitempty"`
}

type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"`
}

type Manager struct {
	mu          sync.RWMutex
	skills      []*Skill
	byName      map[string]*Skill
	activeSkill *Skill
}

func NewManager() *Manager {
	return &Manager{
		skills: []*Skill{},
		byName: make(map[string]*Skill),
	}
}

func (m *Manager) LoadFromDir(dir string) error {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	paths := []string{}
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name == "skill.md" || strings.HasSuffix(name, ".md") {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(paths)

	for _, path := range paths {
		skill, err := loadSkill(path)
		if err != nil {
			continue
		}
		m.add(skill)
	}
	return nil
}

func (m *Manager) LoadAll() error {
	dir, err := config.SkillsDir()
	if err != nil {
		return err
	}
	return m.LoadFromDir(dir)
}

func (m *Manager) Match(userInput string) *Skill {
	lower := strings.ToLower(userInput)
	if strings.TrimSpace(lower) == "" {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *Skill
	bestScore := 0
	for _, s := range m.skills {
		score := matchScore(lower, s)
		if score > bestScore {
			best = s
			bestScore = score
		}
	}
	if bestScore < 3 {
		return nil
	}
	return best
}

func (m *Manager) Get(name string) *Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byName[normalizeName(name)]
}

func (m *Manager) Activate(name string) (*Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byName[normalizeName(name)]
	if s == nil {
		return nil, fmt.Errorf("skill '%s' 不存在。可用 skills: %v", name, m.listNamesLocked())
	}
	m.activeSkill = s
	return s, nil
}

func (m *Manager) ActiveSkill() *Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeSkill
}

func (m *Manager) Deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeSkill = nil
}

func (m *Manager) List() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Skill, len(m.skills))
	copy(out, m.skills)
	return out
}

func (m *Manager) ListNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listNamesLocked()
}

func (m *Manager) BuildSystemPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.activeSkill == nil {
		return ""
	}

	return fmt.Sprintf(`[Active Skill: %s]
The user has activated the skill "%s". Follow the instructions below.

%s

Use the skill only when it helps the current request. Adapt the process to the repository context.`,
		m.activeSkill.Name, m.activeSkill.Name, m.activeSkill.Content)
}

func (m *Manager) add(skill *Skill) {
	key := normalizeName(skill.Name)
	if key == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if old := m.byName[key]; old != nil {
		for i, s := range m.skills {
			if s == old {
				m.skills[i] = skill
				break
			}
		}
	} else {
		m.skills = append(m.skills, skill)
	}
	m.byName[key] = skill
}

func (m *Manager) listNamesLocked() []string {
	names := make([]string, len(m.skills))
	for i, s := range m.skills {
		names[i] = s.Name
	}
	sort.Strings(names)
	return names
}

func loadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fm, body := parseFrontmatter(string(data))
	if fm.Name == "" {
		base := filepath.Base(path)
		fm.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	fm.Name = normalizeName(fm.Name)
	if fm.Name == "" {
		return nil, fmt.Errorf("skill %s 缺少有效名称", path)
	}
	if fm.Description == "" {
		fm.Description = fm.Name
	}

	keywords := collectKeywords(fm.Name, fm.Description, fm.Keywords)
	return &Skill{
		Name:        fm.Name,
		Description: strings.TrimSpace(fm.Description),
		Content:     strings.TrimSpace(body),
		FilePath:    path,
		Keywords:    keywords,
	}, nil
}

func parseFrontmatter(content string) (frontmatter, string) {
	var fm frontmatter
	content = strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(content, "---") {
		return fm, content
	}

	rest := content[3:]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}

	end := strings.Index(rest, "\n---")
	if end == -1 {
		return fm, content
	}
	yamlContent := rest[:end]
	body := rest[end+4:]
	body = strings.TrimPrefix(body, "\r\n")
	body = strings.TrimPrefix(body, "\n")
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return frontmatter{}, body
	}
	return fm, body
}

func matchScore(input string, s *Skill) int {
	score := 0
	if strings.Contains(input, strings.ToLower(s.Name)) {
		score += 5
	}
	for _, kw := range s.Keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(input, strings.ToLower(kw)) {
			if len([]rune(kw)) >= 4 {
				score += 4
			} else {
				score += 3
			}
		}
	}
	return score
}

func collectKeywords(name, desc string, explicit []string) []string {
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.Trim(strings.ToLower(s), " \t\r\n,，。.;；:：()（）[]【】")
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
	}

	add(name)
	for _, kw := range explicit {
		add(kw)
	}
	for _, match := range quotedKeywordRE.FindAllStringSubmatch(desc, -1) {
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				add(match[i])
			}
		}
	}
	for _, token := range regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_-]{2,}`).FindAllString(desc, -1) {
		add(token)
	}
	for _, kw := range []string{
		"发布", "发版", "release", "审查", "检查代码", "code review", "review",
		"测试", "test", "部署", "deploy", "构建", "build", "版本", "tag",
		"提交", "commit", "格式化", "format", "lint", "重构", "refactor",
		"文档", "document", "api", "数据库", "database", "迁移", "migrate",
	} {
		if strings.Contains(strings.ToLower(desc), strings.ToLower(kw)) {
			add(kw)
		}
	}

	out := make([]string, 0, len(seen))
	for kw := range seen {
		out = append(out, kw)
	}
	sort.Strings(out)
	return out
}

func normalizeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
