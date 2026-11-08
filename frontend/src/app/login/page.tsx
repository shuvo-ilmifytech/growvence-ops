"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AlertCircleIcon } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { Me } from "@/lib/types";
import { LogoMarkDark } from "@/components/brand";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

export default function LoginPage() {
  const router = useRouter();
  const qc = useQueryClient();
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    setPending(true);
    setError("");
    try {
      const { user } = await api<{ user: Me }>("/auth/login", {
        method: "POST",
        body: { email: form.get("email"), password: form.get("password") },
      });
      qc.setQueryData(["me"], user);
      // Back to where sign-in was asked for: same-site paths only, so the link can't send anyone elsewhere.
      const next = new URLSearchParams(window.location.search).get("next") ?? "";
      if (next.startsWith("/oauth/")) {
        // Connecting an AI assistant: that page is served by the API, not this app.
        window.location.assign(next);
      } else {
        router.replace(next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/\\") ? next : "/");
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Could not sign in. Check your connection and try again.");
      setPending(false);
    }
  }

  return (
    <div className="grid min-h-svh lg:grid-cols-2">
      <aside className="relative hidden overflow-hidden bg-sidebar p-10 text-white lg:flex lg:flex-col">
        <div className="flex items-center gap-2.5">
          <LogoMarkDark className="h-9" />
        </div>
        <div className="mt-auto max-w-md space-y-4">
          <h2 className="text-3xl font-semibold leading-tight tracking-tight">
            Every project, assignment and client update in one place.
          </h2>
          <p className="text-sm text-white/60">
            Sales hands off projects, the team delivers, and approved updates reach clients on time.
          </p>
        </div>
        {/* Oversized mark as a quiet brand texture */}
        <LogoMarkDark className="pointer-events-none absolute -right-24 -top-16 h-[420px] opacity-[0.07]" />
      </aside>

      <main className="flex items-center justify-center p-6 md:p-10">
        <div className="w-full max-w-sm space-y-8">
          <div className="flex items-center gap-2.5 lg:hidden">
            <LogoMarkDark className="h-8" />
          </div>
          <div className="space-y-1.5">
            <h1 className="text-2xl font-semibold tracking-tight">Sign in</h1>
            <p className="text-sm text-muted-foreground">Use the email and password your admin gave you.</p>
          </div>
          <form onSubmit={onSubmit}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="email">Email</FieldLabel>
                <Input id="email" name="email" type="email" autoComplete="username" placeholder="you@growvence.com" required />
              </Field>
              <Field>
                <FieldLabel htmlFor="password">Password</FieldLabel>
                <Input id="password" name="password" type="password" autoComplete="current-password" required />
              </Field>
              {error && (
                <div role="alert" className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  <AlertCircleIcon className="mt-0.5 size-4 shrink-0" />
                  {error}
                </div>
              )}
              <Button type="submit" size="lg" disabled={pending} className="w-full">
                {pending && <Spinner />}
                {pending ? "Signing in…" : "Sign in"}
              </Button>
            </FieldGroup>
          </form>
          <p className="text-center text-xs text-muted-foreground">Forgot your password? Ask an admin to reset it.</p>
        </div>
      </main>
    </div>
  );
}
