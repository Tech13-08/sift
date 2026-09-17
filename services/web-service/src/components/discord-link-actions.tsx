"use client";

import { useState } from "react";
import { toast } from "sonner";
import { apiSend } from "@/lib/api";
import { Button, ButtonLink } from "@/components/ui/button";

type DiscordLinkActionsProps = {
  serverInvite: string;
  botInvite?: string;
  linked?: boolean;
  size?: "default" | "sm" | "lg";
  onUnlinked?: () => void | Promise<void>;
};

export function DiscordLinkActions({
  serverInvite,
  botInvite,
  linked = false,
  size = "sm",
  onUnlinked,
}: DiscordLinkActionsProps) {
  const [busy, setBusy] = useState(false);

  async function unlink() {
    if (!window.confirm("Unlink Discord? Digests will stay on the web only until you link again.")) {
      return;
    }
    setBusy(true);
    const res = await apiSend("/api/discord", "DELETE");
    setBusy(false);
    if (!res.ok) {
      toast.error("Could not unlink Discord");
      return;
    }
    toast.success("Discord unlinked");
    onUnlinked?.();
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        <ButtonLink href={serverInvite} size={size} variant="secondary" target="_blank" rel="noreferrer">
          Join support server
        </ButtonLink>
        {botInvite ? (
          <ButtonLink href={botInvite} size={size} variant="secondary" target="_blank" rel="noreferrer">
            Add bot to your server
          </ButtonLink>
        ) : null}
        {linked ? (
          <Button size={size} variant="outline" disabled={busy} onClick={unlink}>
            Unlink Discord
          </Button>
        ) : (
          <ButtonLink href="/auth/discord" size={size}>
            Link Discord
          </ButtonLink>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        {linked
          ? "Linked. Digests can DM you. Keep a shared server with the bot so DMs work."
          : "The bot needs a shared server to DM you. Join ours, or add the bot to a server you own - then link."}
      </p>
    </div>
  );
}
