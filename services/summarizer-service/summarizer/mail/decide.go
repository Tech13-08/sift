package mail

import (
	"fmt"
	"strings"
)

func DecisionOutcome(action, stage, why string) string {
	why = strings.TrimSpace(why)
	if why == "" {
		return action + " via " + stage
	}
	return fmt.Sprintf("%s via %s: %s", action, stage, why)
}
