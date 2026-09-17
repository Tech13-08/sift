#!/usr/bin/env python3
"""Migrate flat package main tests into subpackages."""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from refactor_packages import (  # noqa: E402
    TYPE_RENAMES,
    FIELD_RENAMES,
    CONST_MAP,
    FUNC_PKG,
    UTIL_TO_MODEL,
    UTIL_TO_DIGESTLOG,
    apply_renames,
)

# test func name -> destination package (default inferred from body)
TEST_PKG = {
    "TestLatestSlotOnOrBeforeSameDay": "digest",
    "TestLatestSlotOnOrBeforeYesterday": "digest",
    "TestNextSlotAfter": "digest",
    "TestMajorityLocalDateEveningIsToday": "digest",
    "TestMajorityLocalDateMorningIsYesterday": "digest",
    "TestSlotDSTFallBack": "digest",
    "TestOrganizeByMailbox": "digest",
    "TestCollapseSamePayment": "digest",
    "TestClumpPaymentSummaryDropsRedundantCopy": "digest",
    "TestCollapseSamePaymentDespiteGenericWho": "digest",
    "TestCollapseDifferentPaymentsStaySeparate": "digest",
    "TestEmbedsFromFacts": "digest",
    "TestExtractFactsApplicationOutcome": "mail",
    "TestExtractFactsTimeAskAndMoney": "mail",
    "TestExtractFactsJobBlastIsPromo": "mail",
    "TestExtractFactsPromo": "mail",
    "TestApplyBodyOutcomeUsesRejectionInBody": "mail",
    "TestCompileLineAnyNotice": "mail",
    "TestFormatEmailForCategorizePutsBodyFirst": "llm",
    "TestLooksLikeGreeting": "llm",
    "TestCleanDraftDropsAside": "llm",
    "TestDeFirstPerson": "llm",
    "TestYouVoiceInsightKeepsSearcherI": "llm",
    "TestSelectAfterCullRestoresReplyToMe": "llm",
    "TestSelectAfterCullRestoresApplicationAndFinance": "llm",
    "TestSelectAfterCullRestoresPersonalDrops": "llm",
    "TestParseOneEmailCategory": "llm",
    "TestParseUserRule": "llm",
    "TestClipBody": "llm",
    "TestLooksLikeInsight": "rules",
    "TestParseMailQueryRecapWindow": "insights",
    "TestKeepRecapHit": "insights",
    "TestParseMailQueryToday": "insights",
    "TestFormatVectorAndInsightRAGGate": "llm",
    "TestInsightDropsBodyOnlyMention": "insights",
}

# exported renames for tests (camelCase test sources -> exported API)
EXPORT_RENAMES = [
    (r"\blatestSlotOnOrBefore\b", "LatestSlotOnOrBefore"),
    (r"\bnextSlotAfter\b", "NextSlotAfter"),
    (r"\bmajorityLocalDate\b", "MajorityLocalDate"),
    (r"\bsiftedHeader\b", "SiftedHeader"),
    (r"\borganizeByMailbox\b", "OrganizeByMailbox"),
    (r"\bcollapseRelatedFacts\b", "CollapseRelatedFacts"),
    (r"\bclumpPaymentSummary\b", "clumpPaymentSummary"),  # same package digest
    (r"\bembedsFromFacts\b", "EmbedsFromFacts"),
    (r"\bextractFacts\b", "ExtractFacts"),
    (r"\bcompileLine\b", "CompileLine"),
    (r"\bapplyBodyOutcome\b", "ApplyBodyOutcome"),
    (r"\bformatEmailForCategorize\b", "FormatEmailForCategorize"),
    (r"\blooksLikeGreeting\b", "LooksLikeGreeting"),
    (r"\bcleanDraft\b", "CleanDraft"),
    (r"\bdeFirstPerson\b", "DeFirstPerson"),
    (r"\byouVoiceInsight\b", "YouVoiceInsight"),
    (r"\bselectAfterCull\b", "SelectAfterCull"),
    (r"\bextractJSON\b", "ExtractJSON"),
    (r"\bclipBody\b", "ClipBody"),
    (r"\binterpretUserRule\b", "InterpretUserRule"),
    (r"\brulePromptAppendix\b", "RulePromptAppendix"),
    (r"\bparseMailCommand\b", "ParseMailCommand"),
    (r"\bparseRuleEdits\b", "ParseRuleEdits"),
    (r"\bparseIndexList\b", "ParseIndexList"),
    (r"\bmatchingRules\b", "MatchingRules"),
    (r"\bformatRules\b", "FormatRules"),
    (r"\bremovedReply\b", "RemovedReply"),
    (r"\bwithDefaultColorHint\b", "WithDefaultColorHint"),
    (r"\bruleGetsColorHint\b", "RuleGetsColorHint"),
    (r"\blooksLikeSkipPreference\b", "LooksLikeSkipPreference"),
    (r"\blooksLikeHelpRequest\b", "LooksLikeHelpRequest"),
    (r"\blooksLikeJobPreference\b", "LooksLikeJobPreference"),
    (r"\bruleShowsColor\b", "RuleShowsColor"),
    (r"\bsanitizeUserRuleParse\b", "SanitizeUserRuleParse"),
    (r"\busableMutePattern\b", "UsableMutePattern"),
    (r"\busableInstruction\b", "UsableInstruction"),
    (r"\bruleHelpFull\b", "RuleHelpFull"),
    (r"\bruleHelp\b", "RuleHelp"),
    (r"\bcolorForMail\b", "ColorForMail"),
    (r"\bclassifyInbox\b", "ClassifyInbox"),
    (r"\blooksLikeInsight\b", "LooksLikeInsight"),
    (r"\blooksLikeRulePreference\b", "LooksLikeRulePreference"),
    (r"\bparseMailQuery\b", "ParseMailQuery"),
    (r"\bkeepRecapHit\b", "KeepRecapHit"),
    (r"\bformatInsight\b", "FormatInsight"),
    (r"\bformatVector\b", "FormatVector"),
    (r"\blooksSemanticInsight\b", "LooksSemanticInsight"),
    (r"\binsightWantsRAG\b", "InsightWantsRAG"),
    (r"\bmergeInsightHits\b", "MergeInsightHits"),
    (r"\bpartitionInsightHits\b", "PartitionInsightHits"),
    (r"\binsightMatchStrength\b", "insightMatchStrength"),
    (r"\bapplyRules\b", "ApplyRules"),
    (r"\bclaimedByWatch\b", "ClaimedByWatch"),
    (r"\bhygieneFooter\b", "HygieneFooter"),
    (r"\bcontainsAny\b", "model.ContainsAny"),
    (r"\bnamedColors\b", "rules.NamedColors"),
    (r"\bmailVectorsEnabled\b", "llm.MailVectorsEnabled"),
    (r"\boneEmailCategory\b", "oneEmailCategory"),
    (r"\buserRuleParse\b", "model.UserRuleParse"),
]

CTX_FUNCS = {"ApplyRules", "ExtractAllFacts", "ClassifyHideIntent"}


def split_tests(content: str) -> list[tuple[str, str]]:
    parts = re.split(r"(?=^func Test)", content, flags=re.M)
    header = parts[0]
    out = []
    for part in parts[1:]:
        m = re.match(r"^func (Test\w+)", part)
        if not m:
            continue
        out.append((m.group(1), part))
    return header, out


def infer_pkg(test_name: str, body: str) -> str:
    if test_name in TEST_PKG:
        return TEST_PKG[test_name]
    for fn, pkg in FUNC_PKG.items():
        if re.search(r"\b" + fn + r"\(", body):
            return pkg
    return "rules"


def add_imports_for_pkg(content: str, pkg: str) -> str:
    imports = set()
    if pkg != "model" and re.search(r"\bmodel\.", content):
        imports.add("sift/summarizer-service/summarizer/model")
    for imp_pkg in ("digestlog", "mail", "llm", "rules", "digest", "insights", "api"):
        if imp_pkg == pkg:
            continue
        if re.search(r"\b" + imp_pkg + r"\.", content):
            imports.add(f"sift/summarizer-service/summarizer/{imp_pkg}")

    std = []
    for mod in ("context", "database/sql", "encoding/json", "strings", "testing", "time"):
        if re.search(r"\b" + mod.split("/")[-1] + r"\.", content) or (
            mod == "context" and "context.Background" in content
        ):
            std.append(mod)

    if not imports and not std:
        return content

    # strip existing import block
    content = re.sub(r"import \([\s\S]*?\)\n\n", "", content, count=1)

    block = "import (\n"
    for mod in std:
        block += f'\t"{mod}"\n'
    for imp in sorted(imports):
        block += f'\t"{imp}"\n'
    block += ")\n\n"
    content = re.sub(r"(package \w+\n\n)", r"\1" + block, content, count=1)
    return content


def transform_test(body: str, pkg: str) -> str:
    body = apply_renames(body, pkg)
    for old, new in EXPORT_RENAMES:
        body = re.sub(old, new, body)
    # struct literals
    body = re.sub(r"\bingestedMessage\{", "model.IngestedMessage{", body)
    body = re.sub(r"\bmessageFacts\{", "model.MessageFacts{", body)
    body = re.sub(r"\bmailRule\{", "model.MailRule{", body)
    body = re.sub(r"\binsightHit\{", "model.InsightHit{", body)
    body = re.sub(r"\bmailQuery\{", "model.MailQuery{", body)
    body = re.sub(r"\bdefaultEmbedColor\b", "model.DefaultEmbedColor", body)
    body = re.sub(r"\bApplyRules\(", "ApplyRules(context.Background(), ", body)
    if pkg != "rules" and "ApplyRules(context.Background()" in body:
        body = re.sub(r"\bApplyRules\(", "rules.ApplyRules(", body)
    return body


def process_test_file(src_path: str, buckets: dict[str, list[str]]):
    with open(src_path) as f:
        content = f.read()
    _, tests = split_tests(content)
    for name, body in tests:
        pkg = infer_pkg(name, body)
        body = transform_test(body, pkg)
        buckets.setdefault(pkg, []).append(body)


def write_buckets(buckets: dict[str, list[str]]):
    for pkg, bodies in buckets.items():
        content = f"package {pkg}\n\n"
        merged = "\n".join(bodies)
        merged = add_imports_for_pkg(content + merged, pkg)
        dest = os.path.join(ROOT, "summarizer", pkg, pkg+"_test.go")
        if pkg == "digest":
            dest = os.path.join(ROOT, "summarizer", "digest", "digest_test.go")
        elif pkg == "mail":
            dest = os.path.join(ROOT, "summarizer", "mail", "facts_test.go")
        elif pkg == "llm":
            dest = os.path.join(ROOT, "summarizer", "llm", "ollama_test.go")
        elif pkg == "rules":
            dest = os.path.join(ROOT, "summarizer", "rules", "rules_test.go")
        elif pkg == "insights":
            dest = os.path.join(ROOT, "summarizer", "insights", "insights_test.go")
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        with open(dest, "w") as f:
            f.write(merged)
        print("wrote", dest, len(bodies), "tests")


def main():
    src_dir = "/tmp/sift-tests"
    buckets: dict[str, list[str]] = {}
    for name in os.listdir(src_dir):
        if not name.endswith("_test.go"):
            continue
        if name == "discord_test.go":
            continue  # splitDiscord/pageDiscord live in discord-service now
        process_test_file(os.path.join(src_dir, name), buckets)
    write_buckets(buckets)


if __name__ == "__main__":
    main()
