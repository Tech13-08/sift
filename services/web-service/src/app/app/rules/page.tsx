"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { RequireAuth } from "@/components/require-auth";
import { apiGet, apiSend, RULE_COLORS, type Rule } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";

function colorHex(n: number | null) {
  const v = n ?? 0x95a5a6;
  return `#${v.toString(16).padStart(6, "0")}`;
}

function RulesView() {
  const [rules, setRules] = useState<Rule[] | null>(null);
  const [ruleType, setRuleType] = useState("mute");
  const [pattern, setPattern] = useState("");
  const [instruction, setInstruction] = useState("");
  const [color, setColor] = useState<string>("grey");
  const [saving, setSaving] = useState(false);

  async function load() {
    const res = await apiGet<{ rules: Rule[] }>("/api/rules");
    if (res.ok) setRules(res.data.rules);
    else setRules([]);
  }

  useEffect(() => {
    void load();
  }, []);

  async function addRule(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    const colorVal = RULE_COLORS.find((c) => c.name === color)?.value;
    const res = await apiSend<Rule>("/api/rules", "POST", {
      rule_type: ruleType,
      pattern: pattern.trim(),
      instruction: instruction.trim() || undefined,
      color: ruleType === "mute" ? null : colorVal,
    });
    setSaving(false);
    if (!res.ok) {
      toast.error("Could not add rule");
      return;
    }
    toast.success("Rule saved");
    setPattern("");
    setInstruction("");
    await load();
  }

  async function removeRule(id: string) {
    const res = await apiSend(`/api/rules/${id}`, "DELETE");
    if (!res.ok) {
      toast.error("Could not remove rule");
      return;
    }
    toast.success("Removed");
    await load();
  }

  async function setRuleColor(id: string, name: string) {
    const colorVal = RULE_COLORS.find((c) => c.name === name)?.value ?? null;
    const res = await apiSend(`/api/rules/${id}`, "PATCH", { color: colorVal });
    if (!res.ok) {
      toast.error("Could not update color");
      return;
    }
    await load();
  }

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="font-display text-3xl tracking-tight">Rules</h1>
        <p className="mt-2 text-muted-foreground">
          Mute noise or always keep what matters. These also work in Discord.
        </p>
      </div>

      <form onSubmit={addRule} className="rounded-lg border border-border/60 bg-card/40 p-4">
        <FieldGroup>
          <Field>
            <FieldLabel>Type</FieldLabel>
            <Select value={ruleType} onValueChange={(v) => setRuleType(v ?? "mute")}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="mute">Mute</SelectItem>
                  <SelectItem value="always_show">Always show</SelectItem>
                  <SelectItem value="job_filter">Jobs filter</SelectItem>
                  <SelectItem value="instruction">Instruction</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="pattern">Pattern</FieldLabel>
            <Input
              id="pattern"
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              placeholder="e.g. LinkedIn, Factor75, osprey"
              required
            />
          </Field>
          {ruleType === "instruction" ? (
            <Field>
              <FieldLabel htmlFor="instruction">Instruction</FieldLabel>
              <Textarea
                id="instruction"
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
                placeholder="Natural language preference for digests"
              />
            </Field>
          ) : null}
          {ruleType !== "mute" ? (
            <Field>
              <FieldLabel>Color</FieldLabel>
              <Select value={color} onValueChange={(v) => setColor(v ?? "grey")}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {RULE_COLORS.map((c) => (
                      <SelectItem key={c.name} value={c.name}>
                        {c.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          ) : null}
        </FieldGroup>
        <Button type="submit" className="mt-4" disabled={saving}>
          {saving ? "Adding…" : "Add rule"}
        </Button>
      </form>

      {!rules ? (
        <Skeleton className="h-24 w-full" />
      ) : rules.length === 0 ? (
        <Empty className="border border-border/60 bg-card/40">
          <EmptyHeader>
            <EmptyTitle>No rules yet</EmptyTitle>
            <EmptyDescription>
              Mute newsletters or always show a recruiter - start with one pattern.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="animate-sift-stagger flex flex-col gap-2">
          {rules.map((r) => (
            <li
              key={r.id}
              className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card/50 p-4 sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="flex items-start gap-3">
                <span
                  className="mt-1 size-3 shrink-0 rounded-full"
                  style={{ backgroundColor: colorHex(r.color) }}
                  title="color"
                />
                <div>
                  <p className="text-sm font-medium">
                    <span className="text-muted-foreground">{r.rule_type}</span> · {r.pattern}
                  </p>
                  {r.instruction ? (
                    <p className="text-sm text-muted-foreground">{r.instruction}</p>
                  ) : null}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                {r.rule_type !== "mute" ? (
                  <Select
                    value={RULE_COLORS.find((c) => c.value === r.color)?.name || "grey"}
                    onValueChange={(v) => v && setRuleColor(r.id, v)}
                  >
                    <SelectTrigger className="w-28">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {RULE_COLORS.map((c) => (
                          <SelectItem key={c.name} value={c.name}>
                            {c.name}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                ) : null}
                <Button variant="ghost" size="sm" onClick={() => removeRule(r.id)}>
                  Remove
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default function RulesPage() {
  return (
    <RequireAuth>
      {() => <RulesView />}
    </RequireAuth>
  );
}
