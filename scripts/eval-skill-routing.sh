#!/usr/bin/env bash
set -euo pipefail

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-doraigc-postgres}"
POSTGRES_USER="${POSTGRES_USER:-doraigc}"
BUSINESS_DB_NAME="${BUSINESS_DB_NAME:-doraigc}"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$BUSINESS_DB_NAME" -Atc "
SELECT jsonb_build_object(
  'skill_id', id,
  'skill_name', skill_name,
  'skill_scope', skill_scope,
  'version', 'published',
  'status', status,
  'route_hints', route_hints_json
)::text
FROM skills
WHERE deleted_at IS NULL AND status = 'published' AND published_version_id IS NOT NULL
ORDER BY updated_at DESC, id ASC;
" > "$tmp_dir/skills.jsonl"

cat > "$tmp_dir/eval.go" <<'GO'
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type summary struct {
	SkillID    string            `json:"skill_id"`
	SkillName  string            `json:"skill_name"`
	SkillScope string            `json:"skill_scope"`
	Version    string            `json:"version"`
	Status     string            `json:"status"`
	RouteHints map[string]string `json:"route_hints"`
}

type scenario struct {
	Name     string
	Prompt   string
	Expected string
}

type result struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Reason   string `json:"reason"`
	Pass     bool   `json:"pass"`
}

func main() {
	skills, err := loadSkills(os.Args[1])
	if err != nil {
		panic(err)
	}
	scenarios := []scenario{
		{Name: "storyboard_cn", Expected: "sk_seed_storyboard", Prompt: "请给城市香水做一个30秒广告短片，包含三条分镜建议和风险提醒"},
		{Name: "storyboard_visual_plan", Expected: "sk_seed_storyboard", Prompt: "帮我做高端护肤新品主视觉方案，要有镜头氛围和故事板"},
		{Name: "storyboard_en", Expected: "sk_seed_storyboard", Prompt: "Create a storyboard for a product launch video with 3 shots"},
		{Name: "email_negative", Expected: "", Prompt: "帮我写一封给客户的道歉邮件，语气诚恳一些"},
		{Name: "invoice_negative", Expected: "", Prompt: "帮我整理这张发票的报销说明"},
		{Name: "generic_chat", Expected: "", Prompt: "今天适合做什么运动？"},
	}
	results := make([]result, 0, len(scenarios))
	pass := 0
	for _, item := range scenarios {
		actual, reason := route(item.Prompt, skills)
		ok := actual == item.Expected
		if ok {
			pass++
		}
		results = append(results, result{Name: item.Name, Expected: item.Expected, Actual: actual, Reason: reason, Pass: ok})
	}
	encoded, _ := json.MarshalIndent(map[string]any{
		"total": len(results),
		"pass": pass,
		"accuracy": fmt.Sprintf("%.2f", float64(pass)/float64(len(results))),
		"results": results,
	}, "", "  ")
	fmt.Println(string(encoded))
	if pass != len(results) {
		os.Exit(1)
	}
}

func loadSkills(path string) ([]summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []summary
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item summary
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, scanner.Err()
}

func route(prompt string, candidates []summary) (string, string) {
	prompt = strings.ToLower(prompt)
	bestID := ""
	bestReason := ""
	bestScore := -1
	for _, candidate := range candidates {
		if candidate.Status != "published" || blockedByNegativeHint(prompt, candidate.RouteHints) {
			continue
		}
		score, reason := matchScore(prompt, candidate.RouteHints)
		if score > bestScore {
			bestScore = score
			bestID = candidate.SkillID
			bestReason = reason
		}
	}
	if bestScore > 0 {
		return bestID, bestReason
	}
	return "", "no_route_hint_match"
}

func matchScore(prompt string, hints map[string]string) (int, string) {
	score := 0
	reason := ""
	for key, hint := range hints {
		if key == "priority" || key == "negative_keywords" {
			continue
		}
		matches := countHintMatches(prompt, hint)
		if matches == 0 {
			continue
		}
		current := matches*10 + routePriority(hints)
		if current > score {
			score = current
			reason = "route_hint:" + key
		}
	}
	return score, reason
}

func countHintMatches(prompt string, hint string) int {
	count := 0
	for _, item := range splitHints(hint) {
		if strings.Contains(prompt, strings.ToLower(item)) {
			count++
		}
	}
	return count
}

func blockedByNegativeHint(prompt string, hints map[string]string) bool {
	for _, item := range splitHints(hints["negative_keywords"]) {
		if strings.Contains(prompt, strings.ToLower(item)) {
			return true
		}
	}
	return false
}

func splitHints(value string) []string {
	value = strings.NewReplacer("，", ",", "\n", ",", ";", ",", "；", ",").Replace(value)
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func routePriority(hints map[string]string) int {
	value, err := strconv.Atoi(strings.TrimSpace(hints["priority"]))
	if err != nil {
		return 0
	}
	return value
}
GO

go run "$tmp_dir/eval.go" "$tmp_dir/skills.jsonl"
