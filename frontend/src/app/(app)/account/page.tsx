"use client";

import { useState } from "react";
import { ShieldCheckIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { initials } from "@/lib/format";
import { useMe } from "@/lib/session";
import { projectRoleOf } from "@/lib/types";
import { AssistantsCard } from "@/components/account/assistants-card";
import { PageHeader } from "@/components/page-header";
import { RoleBadge } from "@/components/role-badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";

export default function AccountPage() {
  const { data: me } = useMe();
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const d = new FormData(form);
    setFields({});
    if (d.get("new_password") !== d.get("confirm")) {
      setFields({ confirm: "Passwords don't match" });
      return;
    }
    setPending(true);
    try {
      await api("/auth/password", {
        method: "POST",
        body: { current_password: d.get("current_password"), new_password: d.get("new_password") },
      });
      form.reset();
      toast.success("Password changed", { description: "Your other devices were signed out." });
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) {
        setFields(
          Object.fromEntries(
            Object.entries(err.fields).map(([k, v]) => [k, k === "current_password" ? `Current password ${v}` : `Password ${v}`]),
          ),
        );
      } else toast.error(err instanceof Error ? err.message : "Could not change password");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="max-w-2xl space-y-6">
      <PageHeader title="Account" description="Your profile and sign-in details." />

      <Card>
        <CardContent className="flex items-center gap-4">
          {me ? (
            <>
              <Avatar className="size-14">
                <AvatarFallback className="bg-primary text-lg font-semibold text-primary-foreground">
                  {initials(me.name)}
                </AvatarFallback>
              </Avatar>
              <div className="space-y-1.5">
                <div className="text-lg font-semibold leading-none">{me.name}</div>
                <div className="text-sm text-muted-foreground">{me.email}</div>
                <div className="flex gap-1.5">
                  <RoleBadge role={me.role} />
                  {me.is_admin && (
                    <Badge variant="outline" className="gap-1">
                      <ShieldCheckIcon className="size-3" /> Admin
                    </Badge>
                  )}
                </div>
              </div>
            </>
          ) : (
            <Skeleton className="h-14 w-64" />
          )}
        </CardContent>
      </Card>

      <Card>
        <form onSubmit={onSubmit}>
          <CardHeader>
            <CardTitle>Change password</CardTitle>
            <CardDescription>You&apos;ll stay signed in here; other devices will be signed out.</CardDescription>
          </CardHeader>
          <CardContent className="py-6">
            <FieldGroup>
              <Field data-invalid={!!fields.current_password}>
                <FieldLabel htmlFor="current_password">Current password</FieldLabel>
                <Input id="current_password" name="current_password" type="password" autoComplete="current-password" required />
                <FieldError>{fields.current_password}</FieldError>
              </Field>
              <Field data-invalid={!!fields.new_password}>
                <FieldLabel htmlFor="new_password">New password</FieldLabel>
                <Input id="new_password" name="new_password" type="password" autoComplete="new-password" minLength={10} required />
                <FieldDescription>At least 10 characters.</FieldDescription>
                <FieldError>{fields.new_password}</FieldError>
              </Field>
              <Field data-invalid={!!fields.confirm}>
                <FieldLabel htmlFor="confirm">Confirm new password</FieldLabel>
                <Input id="confirm" name="confirm" type="password" autoComplete="new-password" required />
                <FieldError>{fields.confirm}</FieldError>
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter className="justify-end border-t">
            <Button type="submit" disabled={pending}>Update password</Button>
          </CardFooter>
        </form>
      </Card>

      {/* HR doesn't work on projects, which is all the assistant tools cover. */}
      {/* The assistant tools are all about projects, which these roles don't work on. */}
      {me && !!projectRoleOf(me.role) && <AssistantsCard />}
    </div>
  );
}
