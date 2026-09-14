package main

import (
	"fmt"
	"log"
	"strings"
)

func logDecide(action, stage, from, subject, why string) {
	from = clipRunes(collapseSpace(from), 40)
	subject = clipRunes(collapseSpace(subject), 80)
	why = clipRunes(collapseSpace(why), 160)
	if why == "" {
		why = "-"
	}
	log.Printf("decide %s stage=%s from=%q subject=%q why=%q", action, stage, from, subject, why)
}

func decisionOutcome(action, stage, why string) string {
	why = strings.TrimSpace(why)
	if why == "" {
		return action + " via " + stage
	}
	return fmt.Sprintf("%s via %s: %s", action, stage, why)
}
