package mail

import (
	"strings"

	"sift/summarizer-service/summarizer/model"
)

func IsPersonalSender(from string) bool {
	_, domain := splitFrom(from)
	if domain == "" {
		return false
	}
	switch domain {
	case "gmail.com", "googlemail.com", "yahoo.com", "yahoo.co.uk",
		"hotmail.com", "outlook.com", "live.com", "msn.com",
		"icloud.com", "me.com", "mac.com",
		"proton.me", "protonmail.com", "aol.com", "fastmail.com":
		return !looksMarketingList(from)
	default:
		return false
	}
}

func LooksMarketingList(from string) bool {
	return looksMarketingList(from)
}

func looksMarketingList(from string) bool {
	local, domain := splitFrom(from)
	if local == "" {
		return false
	}
	if model.ContainsAny(local, "news", "marketing", "promo", "deals", "newsletter", "offers") {
		return true
	}
	if strings.HasPrefix(domain, "email.") || strings.HasPrefix(domain, "mail.") || strings.HasPrefix(domain, "e.") {
		return true
	}
	return false
}

func SplitFrom(from string) (local, domain string) {
	return splitFrom(from)
}

// SenderAddress returns the lowercased email from a From header, or "".
func SenderAddress(from string) string {
	local, domain := splitFrom(from)
	if local == "" || domain == "" || !strings.Contains(domain, ".") {
		return ""
	}
	return local + "@" + domain
}

func splitFrom(from string) (local, domain string) {
	addr := strings.ToLower(strings.TrimSpace(from))
	if i := strings.Index(addr, "<"); i >= 0 {
		end := strings.Index(addr, ">")
		if end > i {
			addr = addr[i+1 : end]
		}
	}
	local, domain, _ = strings.Cut(addr, "@")
	return local, domain
}
