package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"sift/summarizer-service/summarizer/llm"
	"sift/summarizer-service/summarizer/model"
)

const hideIntentPrompt = `You classify a Sift mail rule. The user may phrase hiding many ways (ignore, skip, mute, avoid, don't include, leave out, never show, not interested, …) or may want to KEEP mail.

JSON only: {"hide":true|false,"mutes":["short literal phrases the email from/subject/body would contain"],"reason":"short"}

hide=true when they want that mail hidden from digests. Then mutes must be full sender emails when they named an address (e.g. "community@extern.com") - never brand words like "extern" or "Factor75".
hide=false for keep/watch/always-show rules, category skips without an email (skip ads / tech news / sign-in), or mixed rules that still want some of that mail kept. For those use hide=false and leave mutes empty (those stay as instructions).`

var hideIntentJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "hide": {"type": "boolean"},
    "mutes": {"type": "array", "items": {"type": "string"}},
    "reason": {"type": "string"}
  },
  "required": ["hide", "mutes"]
}`)

var HideIntentCache sync.Map // string -> model.HideIntent

func ClassifyHideIntent(ctx context.Context, text string) (model.HideIntent, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return model.HideIntent{}, nil
	}
	key := strings.ToLower(text)
	if v, ok := HideIntentCache.Load(key); ok {
		return v.(model.HideIntent), nil
	}
	raw, err := llm.OllamaJSON(ctx, hideIntentPrompt, text, hideIntentJSONSchema, 180, 0.1)
	if err != nil {
		return model.HideIntent{}, err
	}
	var out model.HideIntent
	if err := json.Unmarshal([]byte(llm.ExtractJSON(raw)), &out); err != nil {
		return model.HideIntent{}, fmt.Errorf("hide intent json: %w", err)
	}
	var mutes []string
	seen := map[string]bool{}
	for _, m := range out.Mutes {
		m = NormalizeMutePattern(m)
		if !UsableMutePattern(m) || seen[muteTokenKey(m)] {
			continue
		}
		seen[muteTokenKey(m)] = true
		mutes = append(mutes, m)
	}
	out.Mutes = CollapseRelatedMutes(mutes)
	if out.Hide && len(out.Mutes) == 0 {
		// Model said hide but gave nothing usable - one fallback phrase only.
		for _, n := range SkipInstructionNeedles(model.MailRule{Instruction: text, Pattern: text}) {
			n = NormalizeMutePattern(n)
			if UsableMutePattern(n) {
				out.Mutes = []string{n}
				break
			}
		}
	}
	out.Mutes = CollapseRelatedMutes(out.Mutes)
	HideIntentCache.Store(key, out)
	return out, nil
}

func PromoteHideInstructions(ctx context.Context, p model.UserRuleParse) model.UserRuleParse {
	var ins []string
	for _, s := range p.Instructions {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		hi, err := ClassifyHideIntent(ctx, s)
		if err != nil {
			log.Printf("hide intent: %v", err)
			ins = append(ins, s)
			continue
		}
		if hi.Hide {
			p.Mutes = append(p.Mutes, hi.Mutes...)
			continue
		}
		ins = append(ins, s)
	}
	p.Instructions = ins
	p.Mutes = CollapseRelatedMutes(p.Mutes)
	return SanitizeUserRuleParse(p)
}

func InstructionIsHide(ctx context.Context, r model.MailRule) bool {
	text := strings.TrimSpace(r.Instruction + " " + r.Pattern)
	if text == "" {
		return false
	}
	hi, err := ClassifyHideIntent(ctx, text)
	if err != nil {
		log.Printf("hide intent fallback heuristics: %v", err)
		return IsPureHideInstruction(text)
	}
	return hi.Hide
}
