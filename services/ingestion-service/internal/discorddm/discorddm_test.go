package discorddm

import "testing"

func TestDeadGmailMessage(t *testing.T) {
	t.Setenv("AUTH_PUBLIC_URL", "http://localhost:3000/")
	got := DeadGmailMessage("you@gmail.com")
	want := "Sift cannot read mail for you@gmail.com. Google access expired. Relink Gmail: http://localhost:3000"
	if got != want {
		t.Fatalf("got %q", got)
	}
}
