"use client";

import { useState } from "react";
import { toast } from "sonner";
import { RequireAuth } from "@/components/require-auth";
import { apiSend } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";

function AskView() {
  const [text, setText] = useState("");
  const [reply, setReply] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onAsk(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setReply(null);
    const res = await apiSend<{ reply: string }>("/api/ask", "POST", { text });
    setLoading(false);
    if (!res.ok) {
      toast.error(res.error || "Ask failed");
      return;
    }
    setReply(res.data.reply || "(empty reply)");
  }

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="font-display text-3xl tracking-tight">Ask</h1>
        <p className="mt-2 text-muted-foreground">
          Same as Discord <code className="text-amber">/query</code> - ask about mail in your linked inboxes.
        </p>
      </div>

      <form onSubmit={onAsk} className="flex flex-col gap-4">
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="ask">Question</FieldLabel>
            <Textarea
              id="ask"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Did I get any Hyundai emails today?"
              required
              rows={3}
            />
            <FieldDescription>Brand and sender matches work best.</FieldDescription>
          </Field>
        </FieldGroup>
        <Button type="submit" disabled={loading || !text.trim()} className="w-fit">
          {loading ? "Looking…" : "Ask"}
        </Button>
      </form>

      {reply ? (
        <div className="animate-sift-rise whitespace-pre-wrap rounded-lg border border-border/60 bg-card/50 p-4 text-foreground">
          {reply}
        </div>
      ) : null}
    </div>
  );
}

export default function AskPage() {
  return (
    <RequireAuth>
      {() => <AskView />}
    </RequireAuth>
  );
}
