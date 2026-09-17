"use client";

import { useCallback, useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { apiGet, type Me } from "@/lib/api";
import { AppShell } from "@/components/app-shell";
import { Skeleton } from "@/components/ui/skeleton";

export function RequireAuth({
  children,
  requireMailbox = true,
}: {
  children: (me: Me, reloadMe: () => Promise<void>) => React.ReactNode;
  /** Default true: no Gmail linked → send to Inboxes. */
  requireMailbox?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);

  const reloadMe = useCallback(async () => {
    const res = await apiGet<Me>("/api/me");
    if (!res.ok) {
      router.replace("/?error=auth");
      return;
    }
    setMe(res.data);
    setLoading(false);
  }, [router]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const res = await apiGet<Me>("/api/me");
      if (cancelled) return;
      if (!res.ok) {
        router.replace("/?error=auth");
        return;
      }
      setMe(res.data);
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [router]);

  useEffect(() => {
    if (loading || !me || !requireMailbox) return;
    if (!me.setup?.gmail && pathname !== "/app/inboxes") {
      router.replace("/app/inboxes?need=gmail");
    }
  }, [loading, me, requireMailbox, pathname, router]);

  if (loading || !me) {
    return (
      <div className="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-16">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  if (requireMailbox && !me.setup?.gmail && pathname !== "/app/inboxes") {
    return (
      <div className="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-16">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  return <AppShell me={me}>{children(me, reloadMe)}</AppShell>;
}
