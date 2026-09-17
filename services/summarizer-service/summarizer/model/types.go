package model

import "time"

type IngestedMessage struct {
	ID        string
	Mailbox   string
	From      string
	Subject   string
	Body      string
	ReplyToMe bool
}

type DigestUser struct {
	ID        string
	DiscordID string
	Username  string
	Timezone  string
	Hour      int
	Minute    int
}

const (
	KindPromo  = "promo"
	KindNotice = "notice"
)

type MessageFacts struct {
	Kind        string
	Outcome     string
	Who         string
	What        string
	When        string
	Summary     string
	From        string
	Title       string
	Mailbox     string
	Color       int
	MatchedRule int // 1-based index into ColorableKeepRules; 0 = none
	ReplyToMe   bool
	Claimed     bool
}

const (
	RuleMute        = "mute"
	RuleAlwaysShow  = "always_show"
	RuleJobFilter   = "job_filter"
	RuleInstruction = "instruction"
)

type MailRule struct {
	ID          string
	Type        string
	Pattern     string
	Instruction string
	Color       int
}

type Embed struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color"`
}

type DigestPayload struct {
	Content string  `json:"content"`
	Embeds  []Embed `json:"embeds"`
	Summary string  `json:"summary,omitempty"`
	// Multi-inbox digests: one message, button per mailbox; views swap in place.
	MailboxOrder []string                 `json:"mailbox_order,omitempty"`
	MailboxViews map[string]DigestPayload `json:"mailbox_views,omitempty"`
}

const EmbedsPerMessage = 10 // Discord embed cap per message

const DefaultEmbedColor = 0x95A5A6

type ParsedCommand struct {
	Action    string
	Pattern   string
	Index     int
	ColorName string
	Reply     string
}

type InboxRoute string

const (
	InboxEmpty     InboxRoute = "empty"
	InboxAck       InboxRoute = "ack"
	InboxEdits     InboxRoute = "edits"
	InboxCommand   InboxRoute = "command"
	InboxInsight   InboxRoute = "insight"
	InboxInterpret InboxRoute = "interpret"
)

type RuleEdit struct {
	Action    string
	Index     int
	Indexes   []int
	ColorName string
}

const (
	SlashRule  = "rule"
	SlashQuery = "query"
	SlashRules = "rules"
	SlashHelp  = "help"
	SlashMute  = "mute"
)

type UserRuleParse struct {
	Instructions []string `json:"instructions"`
	Mutes        []string `json:"mutes"`
	Unmutes      []string `json:"unmutes"`
	Removes      []string `json:"removes"`
	Reply        string   `json:"reply"`
}

type MailQuery struct {
	Needle string
	Start  time.Time
	End    time.Time
	Label  string
	Recap  bool
}

type InsightHit struct {
	ID      string
	From    string
	Subject string
	Kind    string
	When    time.Time
	Snippet string
}

type HideIntent struct {
	Hide   bool     `json:"hide"`
	Mutes  []string `json:"mutes"`
	Reason string   `json:"reason"`
}
