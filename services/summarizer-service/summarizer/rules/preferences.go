package rules

import (
	"context"
	"strings"

	"sift/summarizer-service/summarizer/model"
)

func LooksLikeInsight(s string) bool {
	if LooksLikeRulePreference(s) {
		return false
	}
	if LooksLikeRecapInsight(s) {
		return true
	}
	low := strings.ToLower(s)
	if model.ContainsAny(low,
		"include ", "include any", "always show", "always mention", "treat as",
		"mute ", "ignore ", "avoid ", "don't show", "do not show", "don't include",
		"do not include", "don't need", "do not need", "skip mail", "filter out") {
		return false
	}
	asking := model.ContainsAny(low,
		"did i", "do i have", "have i gotten", "have i received",
		"any email", "any mail", "got any", "get any", "have any",
		"was there", "were there",
		"what email", "what mail", "what did i", "what have i",
		"show me", "miss", "check if", "did sift", "anything")
	aboutMail := model.ContainsAny(low, "email", "mail", "inbox", "about") || strings.Contains(low, "from ")
	return asking && aboutMail
}

func LooksLikeRulePreference(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return LooksLikeKeepPreference(s) || LooksLikeSkipPreferenceHeuristic(s)
}

func LooksLikeSkipPreference(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if LooksLikeSkipPreferenceHeuristic(s) {
		return true
	}
	if v, ok := HideIntentCache.Load(strings.ToLower(s)); ok {
		return v.(model.HideIntent).Hide
	}
	if hi, err := ClassifyHideIntent(context.Background(), s); err == nil {
		return hi.Hide
	}
	return false
}

func LooksLikeSkipPreferenceHeuristic(s string) bool {
	low := strings.ToLower(s)
	return model.ContainsAny(low,
		"don't need", "do not need", "don't want", "do not want",
		"don't show", "do not show", "don't mention", "do not mention",
		"don't include", "do not include", "dont include",
		"never show", "never mention", "stop showing", "stop mentioning",
		"mute ", "no more", "skip mail", "ignore mail", "ignore email", "ignore emails",
		"ignore ", "avoid ", "leave out", "leave off", "exclude ", "filter out",
		"not interested", "advertising the product", "not important", "skip ")
}

func LooksLikeKeepPreference(s string) bool {
	low := strings.ToLower(s)
	return model.ContainsAny(low,
		"treat as important", "treat mail", "always show", "always mention",
		"is important", "as important", "are important",
		"include ", "include any", "include mail", "include email",
		"keep mail from", "keep email from", "keep emails from", "keep any mail",
		"watch for", "watch mail", "show mail from", "show email from")
}

func RuleGetsColorHint(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if hi, err := ClassifyHideIntent(context.Background(), s); err == nil {
		return !hi.Hide && LooksLikeKeepPreference(s)
	}
	if LooksLikeSkipPreferenceHeuristic(s) && !LooksLikeKeepPreference(s) {
		return false
	}
	return LooksLikeKeepPreference(s)
}

func LooksLikeRecapInsight(s string) bool {
	low := strings.ToLower(s)
	if model.ContainsAny(low, "is important", "as important", "are important") &&
		!model.ContainsAny(low, "any important", "what important", "important email", "important mail", "anything important") {
		return false
	}
	return model.ContainsAny(low,
		"important email", "important mail", "anything important", "any important",
		"what mattered", "what was important", "what did sift", "that mattered",
		"what happened", "what's important", "whats important", "what's in my inbox",
		"whats in my inbox", "worth seeing", "sift keep", "sift kept")
}
