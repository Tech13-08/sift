package rules

import (
	"regexp"
	"strings"

	"sift/summarizer-service/summarizer/mail"
)

// Full address, or a domain like extern.com / @extern.com. Brands like "extern" alone are not mutes.
var (
	reMuteEmail  = regexp.MustCompile(`(?i)^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)
	reMuteEmailIn = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	reMuteDomain  = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9\-]*[a-z0-9])?\.)+[a-z]{2,}$`)
)

func ExtractMuteEmail(s string) (string, bool) {
	s = strings.Trim(strings.TrimSpace(s), "<>")
	if reMuteEmail.MatchString(s) {
		return strings.ToLower(s), true
	}
	m := reMuteEmailIn.FindString(s)
	if m == "" {
		return "", false
	}
	return strings.ToLower(m), true
}

func extractDomainToken(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimSuffix(s, ".")
	if s == "" || !strings.Contains(s, ".") || !reMuteDomain.MatchString(s) {
		return "", false
	}
	return s, true
}

// NormalizeMuteTarget returns "user@host" or "@host.com". Empty if not a valid mute target.
func NormalizeMuteTarget(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	trimmed := strings.Trim(s, "<>")

	// Exact single email.
	if reMuteEmail.MatchString(strings.TrimSpace(trimmed)) {
		return strings.ToLower(strings.TrimSpace(trimmed))
	}

	// Exact domain: extern.com / @extern.com
	domCandidate := strings.TrimPrefix(strings.TrimSpace(trimmed), "@")
	if !strings.Contains(domCandidate, "@") && !strings.Contains(domCandidate, " ") {
		if domain, ok := extractDomainToken(domCandidate); ok {
			return "@" + domain
		}
	}

	// Phrase: pick first email, else first domain-shaped token.
	if m := reMuteEmailIn.FindString(s); m != "" {
		return strings.ToLower(m)
	}
	for _, tok := range strings.Fields(s) {
		tok = strings.Trim(tok, `.,;:"'<>`)
		if reMuteEmail.MatchString(tok) {
			return strings.ToLower(tok)
		}
		d := strings.TrimPrefix(tok, "@")
		if domain, ok := extractDomainToken(d); ok {
			return "@" + domain
		}
	}
	return ""
}

// NormalizeMuteEmail returns the mute target (email or @domain). Name kept for call sites.
func NormalizeMuteEmail(s string) string {
	return NormalizeMuteTarget(s)
}

func ValidMuteEmail(s string) bool {
	return NormalizeMuteTarget(s) != ""
}

func MuteNeedEmailHelp() string {
	return strings.TrimSpace(`
To hide a sender, use their email or domain — e.g. ` + "`/mute @extern.com`" + ` or ` + "`mute alice@company.com`" + `.
A brand name alone (like "extern") won't work.

To skip a *kind* of mail instead (ads, newsletters, sign-ins), use ` + "`/rule`" + `:
` + "`/rule skip job-site product ads`" + ` or ` + "`/rule ignore new sign-in emails`" + `.
`)
}

func MuteMatchesSender(pattern, from string) bool {
	want := NormalizeMuteTarget(pattern)
	if want == "" {
		return false
	}
	got := mail.SenderAddress(from)
	if got == "" {
		return false
	}
	if strings.HasPrefix(want, "@") {
		return muteDomainCovers(want[1:], got)
	}
	return got == want
}

func muteDomainCovers(domain, email string) bool {
	domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "@"))
	_, host := mail.SplitFrom(email)
	if domain == "" || host == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}
