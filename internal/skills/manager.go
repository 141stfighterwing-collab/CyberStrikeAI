package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// Manager SkillsManager
type Manager struct {
	skillsDir string
	logger    *zap.Logger
	skills    map[string]*Skill // Cache loaded skills
	mu        sync.RWMutex      // Protect skills map from concurrent access
}

// Skill Skill definition
type Skill struct {
	Name        string // Skill name
	Description string // Skill description
	Content     string // Skill content (extracted from SKILL.md)
	Path        string // Skill path
}

// NewManager creates a new Skills manager
func NewManager(skillsDir string, logger *zap.Logger) *Manager {
	return &Manager{
		skillsDir: skillsDir,
		logger:    logger,
		skills:    make(map[string]*Skill),
	}
}

// LoadSkill loads a single skill
func (m *Manager) LoadSkill(skillName string) (*Skill, error) {
	// First try read lock check cache
	m.mu.RLock()
	if skill, exists := m.skills[skillName]; exists {
		m.mu.RUnlock()
		return skill, nil
	}
	m.mu.RUnlock()

	// Build skill path
	skillPath := filepath.Join(m.skillsDir, skillName)
	
	// Check if directory exists
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("skill %s not found", skillName)
	}

	// Find the SKILL.md file
	skillFile := filepath.Join(skillPath, "SKILL.md")
	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		// Try other possible filenames
		alternatives := []string{
			filepath.Join(skillPath, "skill.md"),
			filepath.Join(skillPath, "README.md"),
			filepath.Join(skillPath, "readme.md"),
		}
		found := false
		for _, alt := range alternatives {
			if _, err := os.Stat(alt); err == nil {
				skillFile = alt
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("skill file not found for %s", skillName)
		}
	}

	// Read skill file
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file: %w", err)
	}

	// Parse skill content
	skill := m.parseSkillContent(string(content), skillName, skillPath)
	
	// Use write lock cache skill (double check to avoid repeated loading)
	m.mu.Lock()
	// Check again, maybe other goroutines have been loaded
	if existing, exists := m.skills[skillName]; exists {
		m.mu.Unlock()
		return existing, nil
	}
	m.skills[skillName] = skill
	m.mu.Unlock()

	return skill, nil
}

// LoadSkills batch loading skills
func (m *Manager) LoadSkills(skillNames []string) ([]*Skill, error) {
	var skills []*Skill
	var errors []string

	for _, name := range skillNames {
		skill, err := m.LoadSkill(name)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to load skill %s: %v", name, err))
			m.logger.Warn("Failed to load skill", zap.String("skill", name), zap.Error(err))
			continue
		}
		skills = append(skills, skill)
	}

	if len(errors) > 0 && len(skills) == 0 {
		return nil, fmt.Errorf("failed to load any skills: %s", strings.Join(errors, "; "))
	}

	return skills, nil
}

// ListSkills lists all available skills
func (m *Manager) ListSkills() ([]string, error) {
	if _, err := os.Stat(m.skillsDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	entries, err := os.ReadDir(m.skillsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills directory: %w", err)
	}

	var skills []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		// Check if there is a SKILL.md file
		skillFile := filepath.Join(m.skillsDir, skillName, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			skills = append(skills, skillName)
			continue
		}

		// Try other possible filenames
		alternatives := []string{
			filepath.Join(m.skillsDir, skillName, "skill.md"),
			filepath.Join(m.skillsDir, skillName, "README.md"),
			filepath.Join(m.skillsDir, skillName, "readme.md"),
		}
		for _, alt := range alternatives {
			if _, err := os.Stat(alt); err == nil {
				skills = append(skills, skillName)
				break
			}
		}
	}

	return skills, nil
}

// ParseSkillContent parses skill content
// Support YAML front matter format, similar to goskills
func (m *Manager) parseSkillContent(content, skillName, skillPath string) *Skill {
	skill := &Skill{
		Name: skillName,
		Path: skillPath,
	}

	// Check if there is YAML front matter
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			// Parse front matter (simple implementation, only extract name and description)
			frontMatter := parts[1]
			lines := strings.Split(frontMatter, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "name:") {
					name := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
					name = strings.Trim(name, `"'"`)
					if name != "" {
						skill.Name = name
					}
				} else if strings.HasPrefix(line, "description:") {
					desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
					desc = strings.Trim(desc, `"'"`)
					skill.Description = desc
				}
			}
			// The rest is the content
			if len(parts) == 3 {
				skill.Content = strings.TrimSpace(parts[2])
			}
		} else {
			// Without front matter, the entire content is just skill content
			skill.Content = content
		}
	} else {
		// Without front matter, the entire content is just skill content
		skill.Content = content
	}

	// If content is empty, use description as content
	if skill.Content == "" {
		skill.Content = skill.Description
	}

	return skill
}

// GetSkillContent gets the complete content of the skill (used to inject into the system prompt word)
func (m *Manager) GetSkillContent(skillNames []string) (string, error) {
	skills, err := m.LoadSkills(skillNames)
	if err != nil {
		return "", err
	}

	if len(skills) == 0 {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString("# # Available Skills\n\n")
	builder.WriteString("Before performing the task, please read the following skills content carefully, which contains relevant professional knowledge and methods:\n\n")

	for _, skill := range skills {
		builder.WriteString(fmt.Sprintf("### Skill: %s\n", skill.Name))
		if skill.Description != "" {
			builder.WriteString(fmt.Sprintf("**Description**: %s\n\n", skill.Description))
		}
		builder.WriteString(skill.Content)
		builder.WriteString("\n\n---\n\n")
	}

	return builder.String(), nil
}
