package rules

import (
	"context"
	"log"

	"sift/summarizer-service/summarizer/digestlog"
	"sift/summarizer-service/summarizer/llm"
	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

func ExtractAllFacts(ctx context.Context, messages []model.IngestedMessage, rules []model.MailRule) []model.MessageFacts {
	sys := llm.CategorizeOnePrompt
	if appendix := RulePromptAppendix(rules); appendix != "" {
		sys += "\n\nUser rules:\n" + appendix
	}
	out := make([]model.MessageFacts, len(messages))
	for i, msg := range messages {
		if why, ok := FindMute(ctx, rules, msg, model.MessageFacts{}); ok && !msg.ReplyToMe {
			digestlog.LogDecide(ctx, "skip", "mute", msg.From, msg.Subject, why, msg.Body)
			out[i] = model.MessageFacts{Kind: model.KindPromo, From: msg.From, Mailbox: msg.Mailbox, Outcome: mail.DecisionOutcome("skip", "mute", why)}
			continue
		}
		claimed := ClaimedByWatch(rules, msg, model.MessageFacts{})
		f, err := llm.CategorizeOneEmail(ctx, msg, sys, claimed)
		if err != nil {
			log.Printf("categorize fallback: %v", err)
			f, _ = mail.ExtractFacts(msg)
			if f.Kind == model.KindNotice && f.Title == "" {
				f.Title = model.CollapseSpace(msg.Subject)
			}
			if f.Kind == model.KindNotice && f.Summary == "" {
				f.Summary = mail.CompileLine(f)
			}
			f.Title = mail.DeFirstPerson(f.Title)
			f.Summary = mail.DeFirstPerson(f.Summary)
			if f.Outcome == "" {
				if f.Kind == model.KindPromo {
					f.Outcome = mail.DecisionOutcome("skip", "heuristic", "fallback without qwen")
				} else {
					f.Outcome = mail.DecisionOutcome("keep", "heuristic", "fallback without qwen")
				}
			}
		}
		if f.Who == "" {
			f.Who = mail.NamedWho(msg, mail.SenderWho(msg.From))
		}
		action := "skip"
		if f.Kind == model.KindNotice {
			action = "keep"
		}
		digestlog.LogDecide(ctx, action, "qwen", msg.From, msg.Subject, f.Outcome, msg.Body)
		if msg.ReplyToMe {
			f = mail.KeepReplyToMe(msg, f)
			f.Outcome = mail.DecisionOutcome("keep", "reply", "thread you already wrote in")
			digestlog.LogDecide(ctx, "keep", "reply", msg.From, msg.Subject, "thread you already wrote in", msg.Body)
		}
		if ClaimedByWatch(rules, msg, f) {
			before := f.Kind
			f = mail.KeepWatchedMail(msg, f)
			f.Outcome = mail.DecisionOutcome("keep", "watch", "matches a watch/keep rule")
			if before != model.KindNotice {
				digestlog.LogDecide(ctx, "keep", "watch", msg.From, msg.Subject, "watch rule overrode skip", msg.Body)
			} else {
				digestlog.LogDecide(ctx, "keep", "watch", msg.From, msg.Subject, "matches a watch/keep rule", msg.Body)
			}
		}
		// Drop soft matched_rule stamps that the mail does not corroborate.
		if f.MatchedRule > 0 {
			if confirmed := ConfirmMatchedRule(rules, msg, f); confirmed != f.MatchedRule {
				f.MatchedRule = confirmed
			}
		}
		out[i] = f
		out[i].From = msg.From
		out[i].Mailbox = msg.Mailbox
	}
	return out
}
