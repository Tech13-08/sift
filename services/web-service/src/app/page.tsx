"use client";

import { Suspense, useState, type FormEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { SiftLogo } from "@/components/sift-logo";
import { apiSend } from "@/lib/api";
import { brand } from "@/lib/branding.generated";

const FEEDBACK_URL =
  "https://docs.google.com/forms/d/e/1FAIpQLSfX5w7TvbwAQ61Zoq-vsr0weWSuwoeU_C-xmp92ipgvBIUi4A/viewform";

const USERNAME_RULES = "3-32 characters; letters, numbers, and underscores only (no spaces).";
const PASSWORD_RULES = "At least 8 characters (max 128).";

function LandingInner() {
  const params = useSearchParams();
  const router = useRouter();
  const error = params.get("error");
  const [mode, setMode] = useState<"login" | "register" | "forgot">("login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState("");
  const [formInfo, setFormInfo] = useState("");

  let banner = "";
  if (error === "login_first") {
    banner = "Create an account or sign in first, then link Gmail or Discord.";
  } else if (error === "gmail_taken") {
    banner = "That Gmail inbox is already linked to another Sift account.";
  } else if (error === "discord_taken") {
    banner = "That Discord account is already linked to another Sift account.";
  } else if (error === "google_first" || error === "auth") {
    banner = "Sign-in didn’t finish. Try again.";
  } else if (error === "reset_ok") {
    banner = "Password updated. Sign in with your new password.";
  } else if (error) {
    banner = "Sign-in didn’t finish. Try again.";
  }

  function mapAuthError(code?: string, message?: string) {
    if (message) return message;
    if (code === "username_taken") return "That username is taken.";
    if (code === "invalid_credentials") return "Wrong username or password.";
    if (code === "username") return USERNAME_RULES;
    if (code === "password") return PASSWORD_RULES;
    if (code === "password_mismatch") return "Passwords do not match.";
    if (code === "rate_limited") return "Too many tries. Wait a few minutes.";
    return "Could not complete that. Try again.";
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError("");
    setFormInfo("");

    if (mode === "register") {
      if (password !== passwordConfirm) {
        setFormError("Passwords do not match.");
        return;
      }
    }

    setBusy(true);
    try {
      if (mode === "forgot") {
        const res = await apiSend<{ ok?: boolean; message?: string }>("/api/password/forgot", "POST", {
          username,
        });
        if (!res.ok) {
          setFormError(mapAuthError(res.error, res.message));
          return;
        }
        setFormInfo(
          res.data.message ||
            "If that account has a contact email, we sent a reset link. Check that inbox."
        );
        return;
      }

      const path = mode === "register" ? "/api/register" : "/api/login";
      const body =
        mode === "register"
          ? { username, password, password_confirm: passwordConfirm }
          : { username, password };
      const res = await apiSend(path, "POST", body);
      if (!res.ok) {
        setFormError(mapAuthError(res.error, res.message));
        return;
      }
      router.replace("/app");
      router.refresh();
    } catch {
      setFormError("Network error - check your connection and try again.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="relative flex min-h-full flex-1 flex-col overflow-hidden">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{ backgroundImage: "var(--atmosphere-hero-base)" }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-y-0 right-0 w-full max-w-xl opacity-40 sm:opacity-60"
        style={{ backgroundImage: "var(--atmosphere-hero-accent)" }}
      />

      <div className="relative z-10 flex flex-1 flex-col justify-center px-6 py-16 sm:px-10">
        <div className="mx-auto w-full max-w-md">
          <div className="animate-sift-fade flex items-center gap-4 sm:gap-5">
            <SiftLogo className="size-16 text-foreground sm:size-20" />
            <p className="font-display text-5xl tracking-tight text-foreground sm:text-7xl">{brand.name}</p>
          </div>
          <h1 className="animate-sift-rise mt-6 text-2xl font-medium leading-snug text-foreground sm:text-3xl">
            {brand.tagline}
          </h1>
          <p
            className="animate-sift-rise mt-4 text-base text-muted-foreground sm:text-lg"
            style={{ animationDelay: "0.12s" }}
          >
            Keep what would hurt to miss. Skip the rest. Read digests on the web or in Discord.
          </p>

          {banner ? <p className="animate-sift-fade mt-6 text-sm text-coral">{banner}</p> : null}

          <form
            className="animate-sift-rise mt-8 flex flex-col gap-3"
            style={{ animationDelay: "0.2s" }}
            onSubmit={onSubmit}
          >
            <div className="flex flex-wrap gap-2 text-sm">
              <button
                type="button"
                className={mode === "login" ? "text-amber" : "text-muted-foreground"}
                onClick={() => {
                  setMode("login");
                  setFormError("");
                  setFormInfo("");
                }}
              >
                Sign in
              </button>
              <span className="text-muted-foreground">·</span>
              <button
                type="button"
                className={mode === "register" ? "text-amber" : "text-muted-foreground"}
                onClick={() => {
                  setMode("register");
                  setFormError("");
                  setFormInfo("");
                }}
              >
                Create account
              </button>
              <span className="text-muted-foreground">·</span>
              <button
                type="button"
                className={mode === "forgot" ? "text-amber" : "text-muted-foreground"}
                onClick={() => {
                  setMode("forgot");
                  setFormError("");
                  setFormInfo("");
                }}
              >
                Forgot password
              </button>
            </div>

            {mode === "register" ? (
              <ul className="list-disc space-y-1 pl-5 text-xs text-muted-foreground">
                <li>Username: {USERNAME_RULES}</li>
                <li>Password: {PASSWORD_RULES}</li>
                <li>After signup, link at least one Gmail - that becomes your contact email for resets.</li>
              </ul>
            ) : null}

            {mode === "forgot" ? (
              <p className="text-sm text-muted-foreground">
                Enter your username. We’ll email a reset link to your contact inbox (the Gmail you designated in
                Inboxes).
              </p>
            ) : null}

            <label className="text-sm text-muted-foreground">
              Username
              <input
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                required
                minLength={3}
                maxLength={32}
                pattern="[A-Za-z0-9_]+"
                title={USERNAME_RULES}
              />
            </label>

            {mode !== "forgot" ? (
              <label className="text-sm text-muted-foreground">
                Password
                <input
                  type="password"
                  className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete={mode === "register" ? "new-password" : "current-password"}
                  required
                  minLength={mode === "register" ? 8 : undefined}
                  maxLength={128}
                  title={PASSWORD_RULES}
                />
              </label>
            ) : null}

            {mode === "register" ? (
              <label className="text-sm text-muted-foreground">
                Confirm password
                <input
                  type="password"
                  className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  value={passwordConfirm}
                  onChange={(e) => setPasswordConfirm(e.target.value)}
                  autoComplete="new-password"
                  required
                  minLength={8}
                  maxLength={128}
                />
              </label>
            ) : null}

            {formError ? <p className="text-sm text-coral">{formError}</p> : null}
            {formInfo ? <p className="text-sm text-teal">{formInfo}</p> : null}

            <Button type="submit" size="lg" className="h-11 px-6 text-base" disabled={busy}>
              {busy
                ? "Working…"
                : mode === "register"
                  ? "Create account"
                  : mode === "forgot"
                    ? "Send reset link"
                    : "Sign in"}
            </Button>

            {mode === "login" ? (
              <p className="text-xs text-muted-foreground">
                New here?{" "}
                <button type="button" className="text-amber underline-offset-2 hover:underline" onClick={() => setMode("register")}>
                  Create an account
                </button>
              </p>
            ) : null}
          </form>

          <p className="mt-8 text-sm text-muted-foreground">
            <a
              href={FEEDBACK_URL}
              target="_blank"
              rel="noreferrer"
              className="text-amber underline-offset-4 hover:underline"
            >
              Send feedback
            </a>
            {" · "}
            <a
              href="https://grafana-sift.falaktulsi.com"
              target="_blank"
              rel="noreferrer"
              className="text-amber underline-offset-4 hover:underline"
            >
              Cluster status
            </a>
            {" · "}
            <Link href="/reset-password" className="text-amber underline-offset-4 hover:underline">
              Have a reset link?
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}

export default function HomePage() {
  return (
    <Suspense fallback={<div className="min-h-full" />}>
      <LandingInner />
    </Suspense>
  );
}
