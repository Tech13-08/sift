package insights

import (
	"log"
	"time"

	_ "time/tzdata"
)

func locationOrUTC(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("invalid timezone %q, using UTC: %v", name, err)
		return time.UTC
	}
	return loc
}
