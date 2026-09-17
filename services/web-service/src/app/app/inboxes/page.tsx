"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { RequireAuth } from "@/components/require-auth";
import { apiGet, apiSend, type Mailbox, type Me } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button, ButtonLink } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

function InboxesView({ me, reloadMe }: { me: Me; reloadMe: () => Promise<void> }) {
  const params = useSearchParams();
  const needGmail = params.get("need") === "gmail";
  const [mailboxes, setMailboxes] = useState<Mailbox[] | null>(null);
  const [busyEmail, setBusyEmail] = useState<string | null>(null);

  async function load() {
    const res = await apiGet<{ mailboxes: Mailbox[] }>("/api/mailboxes");
    if (res.ok) setMailboxes(res.data.mailboxes);
    else setMailboxes([]);
  }

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    const err = params.get("error");
    if (err === "gmail_taken") {
      toast.error("That Gmail inbox is already linked to another Sift account.");
    } else if (err === "auth") {
      toast.error("Gmail link didn’t finish. Try again.");
    }
  }, [params]);

  async function unlinkMailbox(email: string) {
    if (
      !window.confirm(
        `Unlink ${email}? Sift will stop watching it and remove mail already stored from this inbox.`
      )
    ) {
      return;
    }
    setBusyEmail(email);
    const res = await apiSend("/api/mailboxes", "DELETE", { email });
    setBusyEmail(null);
    if (!res.ok) {
      toast.error(
        res.message ||
          (res.error === "last_mailbox"
            ? "Keep at least one linked Gmail."
            : res.error === "not_found"
              ? "Inbox not found"
              : "Could not unlink inbox")
      );
      return;
    }
    toast.success(`Unlinked ${email}`);
    await load();
    await reloadMe();
  }

  async function setContact(email: string) {
    setBusyEmail(email);
    const res = await apiSend("/api/contact-email", "POST", { email });
    setBusyEmail(null);
    if (!res.ok) {
      toast.error(res.message || "Could not update contact email");
      return;
    }
    toast.success(`${email} is now your contact email`);
    await load();
    await reloadMe();
  }

  if (!mailboxes) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-28 w-full" />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="font-display text-3xl tracking-tight">Inboxes</h1>
          <p className="mt-2 text-muted-foreground">
            Link at least one Gmail. One inbox is your <span className="text-foreground">contact email</span> for
            password resets (defaults to the first you link).
          </p>
        </div>
        <ButtonLink href="/auth/google">
          {mailboxes.length ? "Link another Gmail" : "Link Gmail"}
        </ButtonLink>
      </div>

      {needGmail || mailboxes.length === 0 ? (
        <p className="rounded-md border border-amber/40 bg-amber/10 px-3 py-2 text-sm text-foreground">
          Link a Gmail inbox to use Sift. That address becomes your contact email until you pick another.
        </p>
      ) : null}

      {mailboxes.length === 0 ? (
        <Empty className="border border-border/60 bg-card/40">
          <EmptyHeader>
            <EmptyTitle>No inbox yet</EmptyTitle>
            <EmptyDescription>Link Gmail to start receiving digests and enable password resets.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="animate-sift-stagger flex flex-col gap-3">
          {mailboxes.map((box) => {
            const isContact =
              box.is_contact ||
              (me.contact_email || me.email || "").toLowerCase() === box.email.toLowerCase();
            return (
              <Card key={box.email}>
                <CardHeader className="flex flex-row items-start justify-between gap-3">
                  <div>
                    <CardTitle className="text-base font-medium">
                      {box.email}
                      {isContact ? (
                        <span className="ml-2 text-xs font-normal text-amber">contact email</span>
                      ) : null}
                    </CardTitle>
                    <CardDescription>
                      {box.status === "relink"
                        ? "Mail from this inbox will not be read until you relink."
                        : box.status === "unknown"
                          ? "Could not verify Google access."
                          : "Watching for new mail."}
                    </CardDescription>
                  </div>
                  <Badge
                    variant="outline"
                    className={
                      box.status === "ok"
                        ? "border-teal/40 text-teal"
                        : box.status === "relink"
                          ? "border-coral/50 text-coral"
                          : ""
                    }
                  >
                    {box.status === "ok" ? "ok" : box.status === "relink" ? "relink" : "unknown"}
                  </Badge>
                </CardHeader>
                <CardContent className="flex flex-wrap gap-2">
                  {box.status === "relink" ? (
                    <ButtonLink href="/auth/google" size="sm">
                      Relink
                    </ButtonLink>
                  ) : null}
                  {!isContact ? (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busyEmail === box.email}
                      onClick={() => setContact(box.email)}
                    >
                      Make contact email
                    </Button>
                  ) : null}
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busyEmail === box.email || mailboxes.length <= 1}
                    onClick={() => unlinkMailbox(box.email)}
                  >
                    Unlink
                  </Button>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}
    </div>
  );
}

export default function InboxesPage() {
  return (
    <RequireAuth requireMailbox={false}>
      {(me, reloadMe) => (
        <Suspense fallback={<Skeleton className="h-28 w-full" />}>
          <InboxesView me={me} reloadMe={reloadMe} />
        </Suspense>
      )}
    </RequireAuth>
  );
}
