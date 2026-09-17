#!/usr/bin/env python3
"""Split flat summarizer package into purpose-based subpackages."""
from __future__ import annotations

import os
import re
import shutil

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SUM = os.path.join(ROOT, "summarizer")

MOVES = {
    "decide.go": "mail",
    "facts.go": "mail",
    "embed.go": "llm",
    "ollama.go": "llm",
    "hide_intent.go": "rules",
    "colors.go": "rules",
    "hygiene.go": "rules",
    "rules.go": "rules",
    "rules_inbox.go": "rules",
    "slash.go": "rules",
    "retention.go": "digest",
    "digest.go": "digest",
    "compile.go": "digest",
    "insights.go": "insights",
    "http_api.go": "api",
    "discord_client.go": "api",
}

STRIP_RE = [
    r"type ingestedMessage struct \{[^}]+\}\n+",
    r"type digestUser struct \{[^}]+\}\n+",
    r"type messageFacts struct \{[^}]+\}\n+",
    r"type mailRule struct \{[^}]+\}\n+",
    r"type digestPayload struct \{[^}]+\}\n+",
    r"type discordEmbed struct \{[^}]+\}\n+",
    r"type parsedCommand struct \{[^}]+\}\n+",
    r"type inboxRoute string\n+",
    r"type ruleEdit struct \{[^}]+\}\n+",
    r"type userRuleParse struct \{[^}]+\}\n+",
    r"type mailQuery struct \{[^}]+\}\n+",
    r"type insightHit struct \{[^}]+\}\n+",
    r"type hideIntent struct \{[^}]+\}\n+",
    r"const \(\n\tkindPromo[^\)]+\)\n+",
    r"const \(\n\truleMute[^\)]+\)\n+",
    r"const \(\n\tinboxEmpty[^\)]+\)\n+",
    r"const \(\n\tslashRule = \"rule\"[^\)]+\)\n+",
    r"const defaultEmbedColor[^\n]+\n",
    r"// Matches Discord[^\n]+\nconst discordEmbedsPerMessage[^\n]+\n",
    r"func collapseSpace\(s string\) string \{[^}]+\}\n+",
    r"func containsAny\(s string, needles \.\.\.string\) bool \{[^}]+\}\n+",
    r"func clipRunes\(s string, max int\) string \{[^}]+\}\n+",
    r"func firstNonEmpty\(vals \.\.\.string\) string \{[^}]+\}\n+",
    r"func nullIfEmpty\(s string\) any \{[^}]+\}\n+",
]

REPL = [
    (r"\bingestedMessage\b", "model.IngestedMessage"),
    (r"\bdigestUser\b", "model.DigestUser"),
    (r"\bmessageFacts\b", "model.MessageFacts"),
    (r"\bmailRule\b", "model.MailRule"),
    (r"\bdigestPayload\b", "model.DigestPayload"),
    (r"\bdiscordEmbed\b", "model.Embed"),
    (r"\bparsedCommand\b", "model.ParsedCommand"),
    (r"\bruleEdit\b", "model.RuleEdit"),
    (r"\binboxRoute\b", "model.InboxRoute"),
    (r"\buserRuleParse\b", "model.UserRuleParse"),
    (r"\bmailQuery\b", "model.MailQuery"),
    (r"\binsightHit\b", "model.InsightHit"),
    (r"\bhideIntent\b", "model.HideIntent"),
    (r"\bkindPromo\b", "model.KindPromo"),
    (r"\bkindNotice\b", "model.KindNotice"),
    (r"\bruleMute\b", "model.RuleMute"),
    (r"\bruleAlwaysShow\b", "model.RuleAlwaysShow"),
    (r"\bruleJobFilter\b", "model.RuleJobFilter"),
    (r"\bruleInstruction\b", "model.RuleInstruction"),
    (r"\binboxEmpty\b", "model.InboxEmpty"),
    (r"\binboxAck\b", "model.InboxAck"),
    (r"\binboxEdits\b", "model.InboxEdits"),
    (r"\binboxCommand\b", "model.InboxCommand"),
    (r"\binboxInsight\b", "model.InboxInsight"),
    (r"\binboxInterpret\b", "model.InboxInterpret"),
    (r"\bslashRule\b", "model.SlashRule"),
    (r"\bslashQuery\b", "model.SlashQuery"),
    (r"\bslashRules\b", "model.SlashRules"),
    (r"\bslashHelp\b", "model.SlashHelp"),
    (r"\bdefaultEmbedColor\b", "model.DefaultEmbedColor"),
    (r"\bdiscordEmbedsPerMessage\b", "model.EmbedsPerMessage"),
    (r"\.id\b", ".ID"),
    (r"\.mailbox\b", ".Mailbox"),
    (r"\.from\b", ".From"),
    (r"\.subject\b", ".Subject"),
    (r"\.body\b", ".Body"),
    (r"\.replyToMe\b", ".ReplyToMe"),
    (r"\{id:", "{ID:"),
    (r"\{mailbox:", "{Mailbox:"),
    (r"\{from:", "{From:"),
    (r"\{subject:", "{Subject:"),
    (r"\{body:", "{Body:"),
    (r"\{replyToMe:", "{ReplyToMe:"),
    (r"\bcollapseSpace\b", "model.CollapseSpace"),
    (r"\bcontainsAny\b", "model.ContainsAny"),
    (r"\bclipRunes\b", "model.ClipRunes"),
    (r"\bfirstNonEmpty\b", "model.FirstNonEmpty"),
    (r"\bnullIfEmpty\b", "model.NullIfEmpty"),
]

# Old name -> exported name in home package (after move)
EXPORT = {
    "decisionOutcome": "DecisionOutcome",
    "extractFacts": "ExtractFacts",
    "compileLine": "CompileLine",
    "extractAllFacts": "ExtractAllFacts",
    "keepWatchedMail": "KeepWatchedMail",
    "keepReplyToMe": "KeepReplyToMe",
    "persistFacts": "PersistFacts",
    "senderWho": "SenderWho",
    "fallbackLine": "FallbackLine",
    "namedWho": "NamedWho",
    "looksJobish": "LooksJobish",
    "looksClosedApplication": "LooksClosedApplication",
    "looksTimeAsk": "LooksTimeAsk",
    "looksMoneyEvent": "LooksMoneyEvent",
    "looksHumanSender": "LooksHumanSender",
    "isPersonalSender": "IsPersonalSender",
    "applyBodyOutcome": "ApplyBodyOutcome",
    "categorizeOneEmail": "CategorizeOneEmail",
    "draftGreeting": "DraftGreeting",
    "cullUnimportant": "CullUnimportant",
    "interpretUserRule": "InterpretUserRule",
    "ollamaJSON": "OllamaJSON",
    "extractJSON": "ExtractJSON",
    "formatEmailForCategorize": "FormatEmailForCategorize",
    "clipBody": "ClipBody",
    "deFirstPerson": "DeFirstPerson",
    "youVoiceInsight": "YouVoiceInsight",
    "cleanDraft": "CleanDraft",
    "looksLikeGreeting": "LooksLikeGreeting",
    "selectAfterCull": "SelectAfterCull",
    "mustKeepFact": "MustKeepFact",
    "ensureVectorSchema": "EnsureVectorSchema",
    "startEmbedBackfill": "StartEmbedBackfill",
    "embedPendingMail": "EmbedPendingMail",
    "formatVector": "FormatVector",
    "searchMailByEmbedding": "SearchMailByEmbedding",
    "mergeInsightHits": "MergeInsightHits",
    "insightWantsRAG": "InsightWantsRAG",
    "looksSemanticInsight": "LooksSemanticInsight",
    "mailVectorsEnabled": "MailVectorsEnabled",
    "parseMailCommand": "ParseMailCommand",
    "classifyInbox": "ClassifyInbox",
    "applyUserCommand": "ApplyUserCommand",
    "applyRulePreference": "ApplyRulePreference",
    "applySlashCommand": "ApplySlashCommand",
    "applyListRules": "ApplyListRules",
    "applyRules": "ApplyRules",
    "loadRules": "LoadRules",
    "findMute": "FindMute",
    "muted": "Muted",
    "alwaysShows": "AlwaysShows",
    "claimedByWatch": "ClaimedByWatch",
    "rulePromptAppendix": "RulePromptAppendix",
    "formatRules": "FormatRules",
    "ruleHelp": "RuleHelp",
    "ruleHelpFull": "RuleHelpFull",
    "parseSlashText": "ParseSlashText",
    "hideIntentCache": "HideIntentCache",
    "classifyHideIntent": "ClassifyHideIntent",
    "promoteHideInstructions": "PromoteHideInstructions",
    "instructionIsHide": "InstructionIsHide",
    "namedColors": "NamedColors",
    "parseColorName": "ParseColorName",
    "colorForMail": "ColorForMail",
    "ruleShowsColor": "RuleShowsColor",
    "skippedByInstruction": "SkippedByInstruction",
    "sanitizeUserRuleParse": "SanitizeUserRuleParse",
    "skipInstructionNeedles": "SkipInstructionNeedles",
    "normalizeMutePattern": "NormalizeMutePattern",
    "muteAlreadyCovered": "MuteAlreadyCovered",
    "collapseRelatedMutes": "CollapseRelatedMutes",
    "mutesForSave": "MutesForSave",
    "instructionRedundant": "InstructionRedundant",
    "muteGeneralizes": "MuteGeneralizes",
    "usableMutePattern": "UsableMutePattern",
    "usableInstruction": "UsableInstruction",
    "isPureHideInstruction": "IsPureHideInstruction",
    "looksLikeSkipPreferenceHeuristic": "LooksLikeSkipPreferenceHeuristic",
    "looksLikeKeepPreference": "LooksLikeKeepPreference",
    "looksLikeSkipPreference": "LooksLikeSkipPreference",
    "ruleGetsColorHint": "RuleGetsColorHint",
    "withDefaultColorHint": "WithDefaultColorHint",
    "removedReply": "RemovedReply",
    "matchingRules": "MatchingRules",
    "parseRuleEdits": "ParseRuleEdits",
    "parseIndexList": "ParseIndexList",
    "looksLikeHelpRequest": "LooksLikeHelpRequest",
    "looksLikeJobPreference": "LooksLikeJobPreference",
    "jobPreferenceCriteria": "JobPreferenceCriteria",
    "hygieneFooter": "HygieneFooter",
    "loadDigestUsers": "LoadDigestUsers",
    "loadDigestUserByDiscordID": "LoadDigestUserByDiscordID",
    "locationOrUTC": "LocationOrUTC",
    "majorityLocalDate": "MajorityLocalDate",
    "siftedHeader": "SiftedHeader",
    "slotOnDate": "SlotOnDate",
    "latestSlotOnOrBefore": "LatestSlotOnOrBefore",
    "nextSlotAfter": "NextSlotAfter",
    "startDigestScheduler": "StartDigestScheduler",
    "runMissedScheduledDigests": "RunMissedScheduledDigests",
    "runManualDigests": "RunManualDigests",
    "runRecentReplay": "RunRecentReplay",
    "runDigest": "RunDigest",
    "loadMessagesSince": "LoadMessagesSince",
    "loadMessagesInWindow": "LoadMessagesInWindow",
    "mailInventory": "MailInventory",
    "buildDigest": "BuildDigest",
    "messageMailboxes": "MessageMailboxes",
    "factsForMailbox": "FactsForMailbox",
    "emptyDigestEmbed": "EmptyDigestEmbed",
    "mailboxDigestPayload": "MailboxDigestPayload",
    "organizeByMailbox": "OrganizeByMailbox",
    "digestMailboxes": "DigestMailboxes",
    "stampMailboxTitle": "StampMailboxTitle",
    "collapseRelatedFacts": "CollapseRelatedFacts",
    "embedsFromFacts": "EmbedsFromFacts",
    "embedTitles": "EmbedTitles",
    "digestTextSummary": "DigestTextSummary",
    "fallbackGreeting": "FallbackGreeting",
    "startMailRetention": "StartMailRetention",
    "pruneIngestedMail": "PruneIngestedMail",
    "looksLikeInsight": "LooksLikeInsight",
    "looksLikeRulePreference": "LooksLikeRulePreference",
    "looksLikeRecapInsight": "LooksLikeRecapInsight",
    "parseMailQuery": "ParseMailQuery",
    "answerInsight": "AnswerInsight",
    "searchImportantMail": "SearchImportantMail",
    "keepRecapHit": "KeepRecapHit",
    "searchIngestedMail": "SearchIngestedMail",
    "draftInsightAnswer": "DraftInsightAnswer",
    "fallbackInsightAnswer": "FallbackInsightAnswer",
    "formatInsight": "FormatInsight",
    "formatInsightSources": "FormatInsightSources",
    "insightSourceLine": "InsightSourceLine",
    "filterInsightHits": "FilterInsightHits",
    "partitionInsightHits": "PartitionInsightHits",
    "cullInsightHits": "CullInsightHits",
    "insightHitKey": "InsightHitKey",
    "mountCommandAPI": "MountCommandAPI",
    "sendDiscordDM": "SendDiscordDM",
    "sendDiscordDigestPayload": "SendDigestPayload",
    "postDiscordService": "PostDiscordService",
    "categorizeOnePrompt": "CategorizeOnePrompt",
}

HOME = {v: k.split("/")[0] if "/" in k else MOVES.get(k + ".go", "") for k, v in []}


def transform(content: str, pkg: str) -> str:
    content = re.sub(r"^package summarizer\s*\n", f"package {pkg}\n", content, count=1)
    for pat in STRIP_RE:
        content = re.sub(pat, "", content, flags=re.DOTALL)
    for a, b in REPL:
        content = re.sub(a, b, content)
    # rename to exported symbols
    for old, new in EXPORT.items():
        content = re.sub(rf"\b{old}\b", new, content)
    return content


def add_imports(content: str, pkg: str) -> str:
    content = re.sub(r"\nimport \([^)]*\)\n", "\n", content, count=1)
    std = sorted(
        set(
            re.findall(
                r'"(?:context|database/sql|encoding/json|fmt|io|log|net/http|net/url|os|regexp|sort|strconv|strings|sync|syscall|time|unicode|unicode/utf8)"',
                content,
            )
        )
    )
    needs = []
    base = "sift/summarizer-service/summarizer/"
    for dep in ["model", "digestlog", "mail", "llm", "rules", "digest", "insights", "api"]:
        if dep == pkg:
            continue
        if re.search(rf"\b{dep}\.", content):
            needs.append(base + dep)
    imps = std + sorted(needs)
    if imps:
        block = "import (\n" + "\n".join("\t" + i for i in imps) + "\n)\n"
        content = re.sub(r"(package \w+\n)\n?", r"\1\n" + block + "\n", content, count=1)
    return content


def prefix_cross_pkg(content: str, pkg: str) -> str:
    # Build home map from EXPORT values -> package dir
    home = {}
    for fn, cap in EXPORT.items():
        for dest, dpkg in MOVES.items():
            pass
    pkg_of = {}
    for fname, dpkg in MOVES.items():
        with open(os.path.join(SUM, dpkg, fname), encoding="utf-8") as f:
            text = f.read()
        for cap in EXPORT.values():
            if re.search(rf"^func {cap}\b", text, re.M):
                pkg_of[cap] = dpkg
        for m in re.finditer(r"^func ([A-Z]\w*)", text, re.M):
            pkg_of[m.group(1)] = dpkg
        for m in re.finditer(r"^var ([A-Z]\w*)", text, re.M):
            pkg_of[m.group(1)] = dpkg
    # digestlog/model special
    for sym, dep in [
        ("WithUser", "digestlog"), ("Logf", "digestlog"), ("LogDecide", "digestlog"),
        ("Clear", "digestlog"), ("Len", "digestlog"), ("SnapshotEntries", "digestlog"),
        ("SnapshotEntriesSince", "digestlog"), ("FilterEntries", "digestlog"),
        ("FilterFromQuery", "digestlog"), ("NormalizeFilter", "digestlog"),
        ("AppendLog", "digestlog"), ("Cap", "digestlog"),
    ]:
        pkg_of[sym] = dep
    for sym in ["IngestedMessage", "DigestUser", "MessageFacts", "MailRule", "DigestPayload", "Embed",
                "ParsedCommand", "RuleEdit", "UserRuleParse", "MailQuery", "InsightHit", "HideIntent",
                "CollapseSpace", "ContainsAny", "ClipRunes", "FirstNonEmpty", "NullIfEmpty",
                "KindPromo", "KindNotice", "RuleMute", "RuleAlwaysShow", "RuleJobFilter", "RuleInstruction",
                "InboxEmpty", "InboxAck", "InboxEdits", "InboxCommand", "InboxInsight", "InboxInterpret",
                "SlashRule", "SlashQuery", "SlashRules", "SlashHelp", "DefaultEmbedColor", "EmbedsPerMessage"]:
        pkg_of[sym] = "model"

    for fn, home in sorted(pkg_of.items(), key=lambda x: -len(x[0])):
        if home == pkg:
            continue
        content = re.sub(rf"(?<![.\w]){re.escape(fn)}\(", f"{home}.{fn}(", content)
    for dep in ["model", "digestlog", "mail", "llm", "rules", "digest", "insights", "api"]:
        content = re.sub(rf"\b{dep}\.{dep}\.", f"{dep}.", content)
    return content


def main():
    for fname, pkg in MOVES.items():
        src = os.path.join(SUM, fname)
        if not os.path.exists(src):
            raise SystemExit(f"missing {src}")
        os.makedirs(os.path.join(SUM, pkg), exist_ok=True)
        with open(src, encoding="utf-8") as f:
            content = transform(f.read(), pkg)
        dst = os.path.join(SUM, pkg, fname)
        with open(dst, "w", encoding="utf-8") as f:
            f.write(content)

    # extractAllFacts -> rules/extract.go
    facts_path = os.path.join(SUM, "mail", "facts.go")
    with open(facts_path, encoding="utf-8") as f:
        facts = f.read()
    m = re.search(r"func ExtractAllFacts[\s\S]+?^}\n", facts, re.M)
    if m:
        extract = m.group(0)
        facts = facts.replace(extract, "")
        with open(facts_path, "w", encoding="utf-8") as f:
            f.write(facts)
        extract = "package rules\n\n" + extract
        with open(os.path.join(SUM, "rules", "extract.go"), "w", encoding="utf-8") as f:
            f.write(extract)

    # api types
    api_types = '''package api

type inboxAPIRequest struct {
	DiscordID string `json:"discord_id"`
	Text      string `json:"text"`
}

type slashAPIRequest struct {
	DiscordID string `json:"discord_id"`
	Name      string `json:"name"`
	Arg       string `json:"arg"`
}

type apiReply struct {
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}
'''
    with open(os.path.join(SUM, "api", "types.go"), "w", encoding="utf-8") as f:
        f.write(api_types)

    # prefix + imports for all subpackages
    for pkg in set(MOVES.values()) | {"api"}:
        for path in os.listdir(os.path.join(SUM, pkg)):
            if not path.endswith(".go"):
                continue
            fpath = os.path.join(SUM, pkg, path)
            with open(fpath, encoding="utf-8") as f:
                content = f.read()
            content = prefix_cross_pkg(content, pkg)
            content = add_imports(content, pkg)
            with open(fpath, "w", encoding="utf-8") as f:
                f.write(content)

    print("split complete")


if __name__ == "__main__":
    main()
