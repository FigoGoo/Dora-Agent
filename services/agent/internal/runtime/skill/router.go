package skill

import "strings"

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
	for _, candidate := range candidates {
		if candidate.Status != "published" {
			continue
		}
		for key, hint := range candidate.RouteHints {
			if hint != "" && strings.Contains(prompt, strings.ToLower(hint)) {
				return RouteResult{Matched: true, Skill: candidate, Reason: "route_hint:" + key}
			}
		}
	}
	for _, candidate := range candidates {
		if candidate.Status == "published" {
			return RouteResult{Matched: true, Skill: candidate, Reason: "published_default"}
		}
	}
	return RouteResult{Matched: false, Reason: "no_published_skill"}
}
