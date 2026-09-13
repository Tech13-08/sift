package main

import (
	"strings"
	"testing"
	"time"
)

func TestLatestSlotOnOrBeforeSameDay(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 13, 14, 0, 0, loc)
	got := latestSlotOnOrBefore(now, loc, 9, 0)
	want := time.Date(2026, 9, 11, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestLatestSlotOnOrBeforeYesterday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, loc)
	got := latestSlotOnOrBefore(now, loc, 9, 0)
	want := time.Date(2026, 9, 10, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNextSlotAfter(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, loc)
	got := nextSlotAfter(now, loc, 9, 0)
	want := time.Date(2026, 9, 12, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestMajorityLocalDateEveningIsToday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 23, 0, 0, 0, loc)
	got := majorityLocalDate(now, loc)
	if got.Year() != 2026 || got.Month() != 9 || got.Day() != 11 {
		t.Fatalf("11pm should be today: %s", got)
	}
	if !strings.Contains(siftedHeader(now, loc), "Friday, September 11, 2026") {
		t.Fatalf("header: %s", siftedHeader(now, loc))
	}
}

func TestMajorityLocalDateMorningIsYesterday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, loc)
	got := majorityLocalDate(now, loc)
	if got.Year() != 2026 || got.Month() != 9 || got.Day() != 10 {
		t.Fatalf("8am should be yesterday: %s", got)
	}
}

func TestSlotDSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-11-01 is DST end in US. 01:30 occurs twice; 09:00 is unambiguous.
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, loc)
	got := latestSlotOnOrBefore(now, loc, 9, 0)
	if got.Hour() != 9 || got.Day() != 1 {
		t.Fatalf("got %s", got)
	}
}
