package mail

import (
	"regexp"
	"strings"
)

var firstPersonSubs = []struct {
	pat  *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\bI'm\b`), "you are"},
	{regexp.MustCompile(`(?i)\bI am\b`), "you are"},
	{regexp.MustCompile(`(?i)\bI was\b`), "you were"},
	{regexp.MustCompile(`(?i)\bI've\b`), "you have"},
	{regexp.MustCompile(`(?i)\bI'll\b`), "you will"},
	{regexp.MustCompile(`(?i)\bI'd\b`), "you would"},
	{regexp.MustCompile(`(?i)\bI\b`), "you"},
	{regexp.MustCompile(`(?i)\bme\b`), "you"},
	{regexp.MustCompile(`(?i)\bmy\b`), "your"},
	{regexp.MustCompile(`(?i)\bmine\b`), "yours"},
}

// Digests talk to the owner ("you"), not as them ("I").
func DeFirstPerson(s string) string {
	s = strings.TrimSpace(s)
	for _, sub := range firstPersonSubs {
		s = sub.pat.ReplaceAllString(s, sub.repl)
	}
	return strings.TrimSpace(s)
}
