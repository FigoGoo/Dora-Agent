package skill

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type SkillPlan struct {
	SkillID     string       `json:"skill_id,omitempty"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Stages      []SkillStage `json:"stages"`
}

type SkillStage struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Goal        string   `json:"goal,omitempty"`
	ToolKeys    []string `json:"tool_keys,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	PauseAfter  bool     `json:"pause_after"`
	Instruction string   `json:"instruction,omitempty"`
}

var (
	tagPattern     = regexp.MustCompile(`(?s)<%s>\s*(.*?)\s*</%s>`)
	stagePattern   = regexp.MustCompile(`^\s*(\d+)[\.\)]\s*(.+)$`)
	toolKeyPattern = regexp.MustCompile(`\*\*\s*([A-Za-z0-9_.-]+)\s*\*\*`)
	dependsPattern = regexp.MustCompile(`depends_on\s*:\s*\[(.*?)\]`)
	pausePattern   = regexp.MustCompile(`pause_after\s*:\s*(true|false)`)
)

func ParseSkill(content string) (*SkillPlan, error) {
	name := extractTag(content, "name")
	description := extractTag(content, "description")
	planner := extractTag(content, "planner")
	if name == "" {
		return nil, fmt.Errorf("skill <name> is required")
	}
	if description == "" {
		return nil, fmt.Errorf("skill <description> is required")
	}
	if planner == "" {
		return nil, fmt.Errorf("skill <planner> is required")
	}

	stages, err := parsePlanner(planner)
	if err != nil {
		return nil, err
	}
	return &SkillPlan{
		Name:        name,
		Description: description,
		Stages:      stages,
	}, nil
}

func ParseSkillReader(r io.Reader) (*SkillPlan, error) {
	var b strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		b.WriteString(scanner.Text())
		b.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ParseSkill(b.String())
}

func extractTag(content string, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(tagPattern.String(), regexp.QuoteMeta(tag), regexp.QuoteMeta(tag)))
	match := re.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func parsePlanner(planner string) ([]SkillStage, error) {
	lines := strings.Split(planner, "\n")
	var stages []SkillStage
	var current *SkillStage

	flush := func() {
		if current != nil {
			stages = append(stages, *current)
		}
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if match := stagePattern.FindStringSubmatch(line); len(match) == 3 {
			flush()
			title, tools := splitStageTitleAndTools(match[2])
			current = &SkillStage{
				Key:        match[1],
				Title:      title,
				Goal:       title,
				ToolKeys:   tools,
				PauseAfter: false,
			}
			continue
		}

		if current == nil {
			continue
		}
		if match := dependsPattern.FindStringSubmatch(line); len(match) == 2 {
			current.DependsOn = parseDepends(match[1])
			continue
		}
		if match := pausePattern.FindStringSubmatch(line); len(match) == 2 {
			current.PauseAfter = match[1] == "true"
			continue
		}
		current.Instruction = strings.TrimSpace(strings.Join([]string{current.Instruction, line}, "\n"))
		current.ToolKeys = appendUnique(current.ToolKeys, extractToolKeys(line)...)
	}
	flush()

	if len(stages) == 0 {
		return nil, fmt.Errorf("planner contains no stages")
	}
	return stages, nil
}

func splitStageTitleAndTools(text string) (string, []string) {
	tools := extractToolKeys(text)
	title := toolKeyPattern.ReplaceAllString(text, "")
	title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(title), "->"))
	title = strings.TrimSpace(strings.TrimSuffix(title, "→"))
	return title, tools
}

func extractToolKeys(text string) []string {
	matches := toolKeyPattern.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			out = appendUnique(out, strings.TrimSpace(match[1]))
		}
	}
	return out
}

func parseDepends(text string) []string {
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, err := strconv.Atoi(part); err == nil {
			out = append(out, part)
			continue
		}
		out = append(out, strings.Trim(part, `"'`))
	}
	return out
}

func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst)+len(values))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		dst = append(dst, v)
		seen[v] = true
	}
	return dst
}
