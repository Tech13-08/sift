package rules

import (
	"context"
	"database/sql"
	"strings"

	"sift/summarizer-service/summarizer/insights"
	"sift/summarizer-service/summarizer/model"
)

// Typed "/rule …" / "/query …" in DM text (not Discord's slash UI).
func ParseSlashText(content string) (name, arg string, ok bool) {
	s := strings.TrimSpace(content)
	if s == "" || !strings.HasPrefix(s, "/") {
		return "", "", false
	}
	s = strings.TrimSpace(s[1:])
	cmd, rest, _ := strings.Cut(s, " ")
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	rest = strings.TrimSpace(rest)
	switch cmd {
	case model.SlashRule, model.SlashQuery, model.SlashRules, model.SlashHelp, model.SlashMute:
		return cmd, rest, true
	default:
		return "", "", false
	}
}

func ApplySlashCommand(ctx context.Context, db *sql.DB, u model.DigestUser, name, arg string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case model.SlashHelp:
		return RuleHelpFull(), nil
	case model.SlashRules:
		return ApplyListRules(ctx, db, u.ID)
	case model.SlashMute:
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return "Hide a sender with `/mute @company.com` or `/mute name@company.com`.", nil
		}
		if NormalizeMuteTarget(arg) == "" {
			return MuteNeedEmailHelp(), nil
		}
		return ApplyUserCommand(ctx, db, u, "mute "+arg)
	case model.SlashQuery:
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return "Ask about your mail — e.g. `/query did I get any Hyundai emails today?`", nil
		}
		return insights.AnswerInsight(ctx, db, u, arg)
	case model.SlashRule:
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return "Say what to keep or skip — e.g. `/rule skip job-site product ads`.", nil
		}
		return ApplyRulePreference(ctx, db, u, arg)
	default:
		return RuleHelp(), nil
	}
}

func ApplyListRules(ctx context.Context, db *sql.DB, userID string) (string, error) {
	if _, err := pruneRedundantRules(ctx, db, userID); err != nil {
		return "", err
	}
	rules, err := LoadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	return FormatRules(rules), nil
}
