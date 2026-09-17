"use client";

import { Suspense, useEffect } from "react";
import { useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { RequireAuth } from "@/components/require-auth";
import { DiscordLinkActions } from "@/components/discord-link-actions";
import type { Me } from "@/lib/api";
import { ButtonLink } from "@/components/ui/button";

function LinkErrorToast() {
  const params = useSearchParams();
  useEffect(() => {
    const err = params.get("error");
    if (err === "discord_taken") {
      toast.error("That Discord account is already linked to another Sift account.");
    } else if (err === "gmail_taken") {
      toast.error("That Gmail inbox is already linked to another Sift account.");
    }
  }, [params]);
  return null;
}

function Checklist({ me, reloadMe }: { me: Me; reloadMe: () => Promise<void> }) {
  const serverInvite = me.discord_server_invite || "https://discord.gg/sDzA28WcjP";
  const steps = [
    {
      done: me.setup.gmail,
      title: "Link Gmail",
      body: "Connect the inboxes Sift should watch. You can add more later.",
      action: (
        <ButtonLink href="/auth/google" size="sm">
          Link Gmail
        </ButtonLink>
      ),
    },
    {
      done: me.setup.schedule,
      title: "Set digest time",
      body: "When should the daily note be ready?",
      action: (
        <ButtonLink href="/app/schedule" size="sm" variant="secondary">
          Choose time
        </ButtonLink>
      ),
    },
    {
      done: me.setup.discord,
      title: "Discord (optional)",
      body: "Get digests in DMs. Share a server with the bot, then link your account.",
      action: (
        <DiscordLinkActions
          serverInvite={serverInvite}
          botInvite={me.discord_bot_invite}
          linked={me.setup.discord}
          onUnlinked={reloadMe}
        />
      ),
      alwaysShowAction: true,
    },
  ];

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="font-display text-3xl tracking-tight">You’re almost live</h1>
        <p className="mt-2 text-muted-foreground">
          Signed in as <span className="text-foreground">{me.username}</span>. Link Gmail and pick a
          digest time - Discord is optional.
        </p>
      </div>
      <ol className="animate-sift-stagger flex flex-col gap-4">
        {steps.map((step, i) => (
          <li
            key={step.title}
            className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card/50 p-4 sm:flex-row sm:items-center sm:justify-between"
          >
            <div className="flex gap-3">
              <span
                className={
                  step.done
                    ? "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-teal/25 text-xs text-teal"
                    : "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border border-border text-xs text-muted-foreground"
                }
              >
                {step.done ? "✓" : i + 1}
              </span>
              <div>
                <p className="font-medium">{step.title}</p>
                <p className="text-sm text-muted-foreground">{step.body}</p>
              </div>
            </div>
            {!step.done || step.alwaysShowAction ? step.action : null}
          </li>
        ))}
      </ol>
    </div>
  );
}

function LiveHome({ me, reloadMe }: { me: Me; reloadMe: () => Promise<void> }) {
  const serverInvite = me.discord_server_invite || "https://discord.gg/sDzA28WcjP";

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="font-display text-3xl tracking-tight">You’re live</h1>
        <p className="mt-2 text-muted-foreground">
          Next digest around{" "}
          <span className="text-amber">
            {me.digest_local_time} · {me.timezone}
          </span>
        </p>
      </div>

      <div className="flex flex-col gap-3 sm:flex-row">
        <ButtonLink href="/app/digests" variant="secondary">
          Last digests
        </ButtonLink>
        <ButtonLink href="/app/rules" variant="secondary">
          Edit rules
        </ButtonLink>
        <ButtonLink href="/auth/google">Link another Gmail</ButtonLink>
      </div>

      <div className="rounded-lg border border-border/60 bg-card/40 p-4">
        <p className="font-medium">{me.setup.discord ? "Discord" : "Want digests in Discord?"}</p>
        {me.setup.discord ? (
          <p className="mt-1 text-sm text-muted-foreground">
            Linked. `/rule`, `/query`, and `/help` work in the bot DM.
          </p>
        ) : null}
        <div className="mt-3">
          <DiscordLinkActions
            serverInvite={serverInvite}
            botInvite={me.discord_bot_invite}
            linked={me.setup.discord}
            onUnlinked={reloadMe}
          />
        </div>
      </div>

      {me.mailboxes_need_relink ? (
        <p className="text-sm text-coral">
          One or more inboxes need a Google relink before mail will flow again.
        </p>
      ) : null}
    </div>
  );
}

export default function AppHomePage() {
  return (
    <RequireAuth>
      {(me, reloadMe) => (
        <>
          <Suspense fallback={null}>
            <LinkErrorToast />
          </Suspense>
          {me.setup.gmail && me.setup.schedule ? (
            <LiveHome me={me} reloadMe={reloadMe} />
          ) : (
            <Checklist me={me} reloadMe={reloadMe} />
          )}
        </>
      )}
    </RequireAuth>
  );
}
