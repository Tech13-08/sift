"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { apiSend, type Me } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { SiftLogo } from "@/components/sift-logo";
import { cn } from "@/lib/utils";
import { brand } from "@/lib/branding.generated";

const FEEDBACK_URL =
  "https://docs.google.com/forms/d/e/1FAIpQLSfX5w7TvbwAQ61Zoq-vsr0weWSuwoeU_C-xmp92ipgvBIUi4A/viewform";

const links = [
  { href: "/app", label: "Home" },
  { href: "/app/inboxes", label: "Inboxes" },
  { href: "/app/schedule", label: "Schedule" },
  { href: "/app/rules", label: "Rules" },
  { href: "/app/digests", label: "Digests" },
  { href: "/app/ask", label: "Ask" },
];

export function AppShell({ me, children }: { me: Me; children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const setupDone = me.setup.gmail && me.setup.schedule;

  async function logout() {
    await apiSend("/api/logout", "POST");
    router.push("/");
    router.refresh();
  }

  return (
    <div className="flex min-h-full flex-col">
      {me.mailboxes_need_relink ? (
        <div className="border-b border-coral/40 bg-coral/15 px-4 py-2 text-center text-sm text-foreground">
          Google access expired for an inbox.{" "}
          <a href="/auth/google" className="font-medium text-amber underline-offset-4 hover:underline">
            Relink Gmail
          </a>
        </div>
      ) : null}

      <header className="border-b border-border/60 px-4 py-4">
        <div className="mx-auto flex max-w-3xl items-center justify-between gap-4">
          <Link href="/app" className="inline-flex items-center gap-2.5 text-foreground">
            <SiftLogo className="size-8" />
            <span className="font-display text-2xl tracking-tight">{brand.name}</span>
          </Link>
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <Link
              href="/app/account"
              className={cn(
                "max-w-[10rem] truncate text-foreground underline-offset-4 hover:text-amber hover:underline sm:max-w-none",
                pathname.startsWith("/app/account") && "text-amber"
              )}
              title="Account settings"
            >
              {me.username}
            </Link>
            <Button variant="ghost" size="sm" onClick={logout}>
              Log out
            </Button>
          </div>
        </div>
        {setupDone ? (
          <nav className="mx-auto mt-4 flex max-w-3xl gap-1 overflow-x-auto pb-1">
            {links.map((l) => {
              const active = pathname === l.href || (l.href !== "/app" && pathname.startsWith(l.href));
              return (
                <Link
                  key={l.href}
                  href={l.href}
                  className={cn(
                    "rounded-md px-3 py-1.5 text-sm whitespace-nowrap transition-colors",
                    active
                      ? "bg-secondary text-foreground"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  {l.label}
                </Link>
              );
            })}
          </nav>
        ) : null}
      </header>

      <main className="mx-auto w-full max-w-3xl flex-1 px-4 py-8">{children}</main>

      <footer className="border-t border-border/40 px-4 py-4 text-center text-xs text-muted-foreground">
        <a
          href={FEEDBACK_URL}
          target="_blank"
          rel="noreferrer"
          className="text-amber underline-offset-4 hover:underline"
        >
          Send feedback
        </a>
      </footer>
    </div>
  );
}
