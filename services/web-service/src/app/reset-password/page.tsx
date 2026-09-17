"use client";

import { Suspense, useState, type FormEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { SiftLogo } from "@/components/sift-logo";
import { apiSend } from "@/lib/api";

const PASSWORD_RULES = "At least 8 characters (max 128).";

function ResetInner() {
  const params = useSearchParams();
  const router = useRouter();
  const tokenFromUrl = params.get("token") || "";
  const [token, setToken] = useState(tokenFromUrl);
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (password !== passwordConfirm) {
      setError("Passwords do not match.");
      return;
    }
    setBusy(true);
    try {
      const res = await apiSend("/api/password/reset", "POST", {
        token,
        password,
        password_confirm: passwordConfirm,
      });
      if (!res.ok) {
        setError(res.message || res.error || "Could not reset password.");
        return;
      }
      router.replace("/?error=reset_ok");
      router.refresh();
    } catch {
      setError("Network error - try again.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto flex min-h-full max-w-md flex-col justify-center gap-6 px-6 py-16">
      <div className="flex items-center gap-3">
        <SiftLogo className="size-10 text-foreground" />
        <p className="font-display text-3xl tracking-tight">Reset password</p>
      </div>
      <p className="text-sm text-muted-foreground">
        {PASSWORD_RULES} Enter the new password twice.
      </p>
      <form className="flex flex-col gap-3" onSubmit={onSubmit}>
        {!tokenFromUrl ? (
          <label className="text-sm text-muted-foreground">
            Reset token
            <input
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              required
            />
          </label>
        ) : null}
        <label className="text-sm text-muted-foreground">
          New password
          <input
            type="password"
            className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
            minLength={8}
            maxLength={128}
          />
        </label>
        <label className="text-sm text-muted-foreground">
          Confirm password
          <input
            type="password"
            className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            value={passwordConfirm}
            onChange={(e) => setPasswordConfirm(e.target.value)}
            autoComplete="new-password"
            required
            minLength={8}
            maxLength={128}
          />
        </label>
        {error ? <p className="text-sm text-coral">{error}</p> : null}
        <Button type="submit" disabled={busy || !token}>
          {busy ? "Saving…" : "Set new password"}
        </Button>
      </form>
      <Link href="/" className="text-sm text-amber underline-offset-4 hover:underline">
        Back to sign in
      </Link>
    </div>
  );
}

export default function ResetPasswordPage() {
  return (
    <Suspense fallback={<div className="min-h-full" />}>
      <ResetInner />
    </Suspense>
  );
}
