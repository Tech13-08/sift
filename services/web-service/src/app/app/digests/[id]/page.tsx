"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { RequireAuth } from "@/components/require-auth";
import { apiGet, type DigestListItem, type DigestMessage } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";

function DigestDetail() {
  const params = useParams<{ id: string }>();
  const [digest, setDigest] = useState<DigestListItem | null>(null);
  const [messages, setMessages] = useState<DigestMessage[] | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    (async () => {
      const res = await apiGet<{ digest: DigestListItem; messages: DigestMessage[] }>(
        `/api/digests/${params.id}`
      );
      if (!res.ok) {
        setError(true);
        return;
      }
      setDigest(res.data.digest);
      setMessages(res.data.messages);
    })();
  }, [params.id]);

  const kept = useMemo(
    () => (messages || []).filter((m) => m.kind === "notice" || !m.kind),
    [messages]
  );
  const skipped = useMemo(
    () => (messages || []).filter((m) => m.kind === "promo"),
    [messages]
  );

  const byMailbox = useMemo(() => {
    const map = new Map<string, DigestMessage[]>();
    for (const m of kept) {
      const key = m.mailbox || "inbox";
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(m);
    }
    return [...map.entries()];
  }, [kept]);

  if (error) {
    return <p className="text-coral">Digest not found.</p>;
  }
  if (!digest || !messages) {
    return <Skeleton className="h-40 w-full" />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Link href="/app/digests" className="text-sm text-muted-foreground hover:text-foreground">
          ← Digests
        </Link>
        <h1 className="font-display mt-3 text-3xl tracking-tight">Digest</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {new Date(digest.created_at).toLocaleString()} · {digest.kind}
        </p>
        {digest.summary ? (
          <p className="mt-4 whitespace-pre-wrap text-foreground/90">{digest.summary}</p>
        ) : null}
      </div>

      {byMailbox.length === 0 ? (
        <p className="text-muted-foreground">No kept mail in this digest.</p>
      ) : (
        byMailbox.map(([mailbox, items]) => (
          <section key={mailbox} className="flex flex-col gap-3">
            <h2 className="text-sm font-medium text-teal">{mailbox}</h2>
            <ul className="flex flex-col gap-2">
              {items.map((m) => (
                <li key={m.id} className="rounded-lg border border-border/60 bg-card/50 p-4">
                  <p className="font-medium text-amber">
                    {m.fact_summary || m.subject || "(no subject)"}
                  </p>
                  <p className="mt-1 text-sm text-muted-foreground">{m.from_address}</p>
                  {m.outcome ? (
                    <p className="mt-2 text-xs text-muted-foreground">{m.outcome}</p>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>
        ))
      )}

      {skipped.length > 0 ? (
        <details className="rounded-lg border border-border/40 bg-card/30 p-4">
          <summary className="cursor-pointer text-sm text-muted-foreground">
            Skipped noise ({skipped.length})
          </summary>
          <ul className="mt-3 flex flex-col gap-2">
            {skipped.map((m) => (
              <li key={m.id} className="text-sm text-muted-foreground">
                <Badge variant="outline" className="mr-2">
                  skip
                </Badge>
                {m.from_address} - {m.subject}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
}

export default function DigestDetailPage() {
  return (
    <RequireAuth>
      {() => <DigestDetail />}
    </RequireAuth>
  );
}
