"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { RequireAuth } from "@/components/require-auth";
import { apiSend, type Me } from "@/lib/api";
import { Button } from "@/components/ui/button";

const PASSWORD_RULES = "At least 8 characters (max 128).";

function AccountView({ me }: { me: Me }) {
  const router = useRouter();
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [newPasswordConfirm, setNewPasswordConfirm] = useState("");
  const [changingPass, setChangingPass] = useState(false);

  async function changePassword(e: FormEvent) {
    e.preventDefault();
    if (newPassword !== newPasswordConfirm) {
      toast.error("Passwords do not match.");
      return;
    }
    setChangingPass(true);
    const res = await apiSend("/api/password/change", "POST", {
      current_password: currentPassword,
      password: newPassword,
      password_confirm: newPasswordConfirm,
    });
    setChangingPass(false);
    if (!res.ok) {
      toast.error(res.message || "Could not change password");
      return;
    }
    toast.success("Password updated");
    setCurrentPassword("");
    setNewPassword("");
    setNewPasswordConfirm("");
  }

  async function deleteAccount() {
    if (deleteConfirm.trim().toLowerCase() !== "delete") {
      toast.error('Type "delete" to confirm');
      return;
    }
    if (
      !window.confirm(
        "Permanently delete your Sift account and all digests, rules, and linked mailboxes?"
      )
    ) {
      return;
    }
    setDeleting(true);
    const res = await apiSend("/api/account", "DELETE", { confirm: "delete" });
    setDeleting(false);
    if (!res.ok) {
      toast.error("Could not delete account");
      return;
    }
    toast.success("Account deleted");
    router.replace("/");
    router.refresh();
  }

  return (
    <div className="flex flex-col gap-10">
      <div>
        <h1 className="font-display text-3xl tracking-tight">Account</h1>
        <p className="mt-2 text-muted-foreground">
          Signed in as <span className="text-foreground">{me.username}</span>
          {me.contact_email || me.email ? (
            <>
              {" "}
              · contact{" "}
              <span className="text-foreground">{me.contact_email || me.email}</span>
            </>
          ) : null}
          . Manage password resets’ contact inbox under{" "}
          <a href="/app/inboxes" className="text-amber underline-offset-4 hover:underline">
            Inboxes
          </a>
          .
        </p>
      </div>

      <section className="rounded-lg border border-border/60 bg-card/40 p-4">
        <h2 className="font-medium text-foreground">Change password</h2>
        <p className="mt-1 text-sm text-muted-foreground">{PASSWORD_RULES}</p>
        <form className="mt-4 flex max-w-sm flex-col gap-3" onSubmit={changePassword}>
          <label className="text-sm text-muted-foreground">
            Current password
            <input
              type="password"
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              autoComplete="current-password"
              required
            />
          </label>
          <label className="text-sm text-muted-foreground">
            New password
            <input
              type="password"
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              autoComplete="new-password"
              required
              minLength={8}
              maxLength={128}
            />
          </label>
          <label className="text-sm text-muted-foreground">
            Confirm new password
            <input
              type="password"
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              value={newPasswordConfirm}
              onChange={(e) => setNewPasswordConfirm(e.target.value)}
              autoComplete="new-password"
              required
              minLength={8}
              maxLength={128}
            />
          </label>
          <Button type="submit" disabled={changingPass}>
            {changingPass ? "Saving…" : "Update password"}
          </Button>
        </form>
      </section>

      <section className="rounded-lg border border-coral/40 bg-coral/10 p-4">
        <h2 className="font-medium text-foreground">Delete account</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Removes your Sift account, linked Discord, Gmail tokens, digests, rules, and stored mail. This cannot be
          undone.
        </p>
        <label className="mt-4 block text-sm text-muted-foreground">
          Type <span className="text-foreground">delete</span> to confirm
          <input
            className="mt-1.5 w-full max-w-xs rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            value={deleteConfirm}
            onChange={(e) => setDeleteConfirm(e.target.value)}
            autoComplete="off"
            placeholder="delete"
          />
        </label>
        <Button
          className="mt-3"
          variant="destructive"
          disabled={deleting || deleteConfirm.trim().toLowerCase() !== "delete"}
          onClick={deleteAccount}
        >
          Delete my account
        </Button>
      </section>
    </div>
  );
}

export default function AccountPage() {
  return (
    <RequireAuth requireMailbox={false}>
      {(me) => <AccountView me={me} />}
    </RequireAuth>
  );
}
