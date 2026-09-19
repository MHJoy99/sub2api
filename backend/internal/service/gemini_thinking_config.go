package service

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var gemini3EffortToLevel = map[string]string{
	"low":    "low",
	"medium": "medium",
	"high":   "high",
	"xhigh":  "high",
	"x-high": "high",
	"max":    "high",
}

// normalizeGeminiThinkingConfig enforces Gemini 3.x thinkingLevel constraints
// at the final request boundary before dispatch.
// Gemini 3.x models reject thinkingBudget when thinkingLevel is absent or reject
// having both fields simultaneously. This normalizer removes thinkingBudget and
// sets thinkingLevel accurately according to original client effort or fallback budgets.
func normalizeGeminiThinkingConfig(body []byte, mappedModel string, originalBody []byte) ([]byte, error) {
	if len(body) == 0 || !antigravity.IsGemini3OrNewer(mappedModel) {
		return body, nil
	}

	// Only process if generationConfig.thinkingConfig exists
	if !gjson.GetBytes(body, "generationConfig.thinkingConfig").Exists() {
		return body, nil
	}

	// 1. Try explicit effort from originalBody
	var explicitEffort string
	if len(originalBody) > 0 {
		if res := gjson.GetBytes(originalBody, "reasoning.effort"); res.Exists() && strings.TrimSpace(res.String()) != "" {
			explicitEffort = strings.ToLower(strings.TrimSpace(res.String()))
		} else if res := gjson.GetBytes(originalBody, "reasoning_effort"); res.Exists() && strings.TrimSpace(res.String()) != "" {
			explicitEffort = strings.ToLower(strings.TrimSpace(res.String()))
		} else if res := gjson.GetBytes(originalBody, "output_config.effort"); res.Exists() && strings.TrimSpace(res.String()) != "" {
			explicitEffort = strings.ToLower(strings.TrimSpace(res.String()))
		}
	}

	var targetLevel string
	if explicitEffort != "" {
		if lvl, ok := gemini3EffortToLevel[explicitEffort]; ok {
			targetLevel = lvl
		}
	}

	// 2. If no explicit effort, check existing thinkingLevel or budget fallback
	if targetLevel == "" {
		if existingLevel := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingLevel").String())); existingLevel != "" {
			if lvl, ok := gemini3EffortToLevel[existingLevel]; ok {
				targetLevel = lvl
			}
		}
	}

	if targetLevel == "" {
		budget := gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingBudget").Int()
		if budget > 0 && budget <= 4096 {
			targetLevel = "low"
		} else if budget > 0 && budget <= 12288 {
			targetLevel = "medium"
		} else {
			targetLevel = "high"
		}
	}

	// Modify JSON
	resBody, err := sjson.DeleteBytes(body, "generationConfig.thinkingConfig.thinkingBudget")
	if err != nil {
		return nil, fmt.Errorf("delete thinkingBudget: %w", err)
	}
	resBody, err = sjson.SetBytes(resBody, "generationConfig.thinkingConfig.thinkingLevel", targetLevel)
	if err != nil {
		return nil, fmt.Errorf("set thinkingLevel: %w", err)
	}
	resBody, err = sjson.SetBytes(resBody, "generationConfig.thinkingConfig.includeThoughts", true)
	if err != nil {
		return nil, fmt.Errorf("set includeThoughts: %w", err)
	}

	return resBody, nil
}
