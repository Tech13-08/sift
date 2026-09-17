"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { RequireAuth } from "@/components/require-auth";
import { apiGet, type DigestListItem } from "@/lib/api";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

function DigestsList() {
  const [digests, setDigests] = useState<DigestListItem[] | null>(null);

  useEffect(() => {
    (async () => {
      const res = await apiGet<{ digests: DigestListItem[] }>("/api/digests?limit=30");
      if (res.ok) setDigests(res.data.digests);
      else setDigests([]);
    })();
  }, []);

  if (!digests) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-20 w-full" />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="font-display text-3xl tracking-tight">Digests</h1>
        <p className="mt-2 text-muted-foreground">Re-read what Sift sent to Discord.</p>
      </div>

      {digests.length === 0 ? (
        <Empty className="border border-border/60 bg-card/40">
          <EmptyHeader>
            <EmptyTitle>No digests yet</EmptyTitle>
            <EmptyDescription>After your first daily note, it will show up here.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="animate-sift-stagger flex flex-col gap-2">
          {digests.map((d) => (
            <li key={d.id}>
              <Link
                href={`/app/digests/${d.id}`}
                className="block rounded-lg border border-border/60 bg-card/50 p-4 transition-colors hover:border-amber/40"
              >
                <p className="text-sm text-muted-foreground">
                  {new Date(d.created_at).toLocaleString()} · {d.kind}
                </p>
                <p className="mt-1 line-clamp-2 text-foreground">
                  {d.summary?.trim() || "Digest"}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default function DigestsPage() {
  return (
    <RequireAuth>
      {() => <DigestsList />}
    </RequireAuth>
  );
}
