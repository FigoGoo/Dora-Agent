package skill

import (
	"strconv"
	"strings"
)

type Summary struct {
	SkillID    string
	SkillName  string
	SkillScope string
	Version    string
	Status     string
	RouteHints map[string]string
}

type RouteResult struct {
	Matched bool
	Skill   Summary
	Reason  string
}

type Router struct{}

func NewRouter() Router {
	return Router{}
}

func (Router) Route(prompt string, candidates []Summary) RouteResult {
	prompt = strings.ToLower(prompt)
	var best *RouteResult
	bestScore := -1
	for _, candidate := range candidates {
		if candidate.Status != "published" {
			continue
		}
		if blockedByNegativeHint(prompt, candidate.RouteHints) {
			continue
		}
		score, reason := matchScore(prompt, candidate.RouteHints)
		if score <= 0 {
			continue
		}
		if score > bestScore {
			result := RouteResult{Matched: true, Skill: candidate, Reason: reason}
			best = &result
			bestScore = score
		}
	}
	if best != nil {
		return *best
	}
	if hasPublished(candidates) {
		return RouteResult{Matched: false, Reason: "no_route_hint_match"}
	}
	return RouteResult{Matched: false, Reason: "no_published_skill"}
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

func hasPublished(candidates []Summary) bool {
	for _, candidate := range candidates {
		if candidate.Status == "published" {
			return true
		}
	}
	return false
}
