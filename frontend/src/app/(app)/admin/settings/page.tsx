"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellIcon, CheckCircle2Icon, GlobeIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import type { Settings } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field";
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { SheetBackup } from "@/components/admin/sheet-backup";

const TIMEZONES = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : ["Asia/Dhaka", "UTC"];

export default function SettingsPage() {
  const qc = useQueryClient();
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api<{ settings: Settings }>("/settings").then((r) => r.settings),
  });
  const save = useMutation({
    mutationFn: ({ key, value }: { key: keyof Settings; value: unknown }) =>
      api(`/admin/settings/${key}`, { method: "PUT", body: { value } }),
    onSuccess: () => {
      toast.success("Setting saved");
      qc.invalidateQueries({ queryKey: ["settings"] });
    },
    onError: (err) => toast.error(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : err.message),
  });
  const s = settings.data;

  return (
    <div className="max-w-3xl space-y-6">
      <PageHeader title="Settings" description="Workspace-wide rules. Changes apply to everyone immediately." />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <CheckCircle2Icon className="size-4 text-primary" /> Update approvals
          </CardTitle>
          <CardDescription>Order page and Delivery updates always need Team Leader approval.</CardDescription>
        </CardHeader>
        <CardContent>
          {s ? (
            <Field orientation="horizontal" className="rounded-lg border p-4">
              <FieldContent>
                <FieldLabel htmlFor="inbox-approval">Inbox updates require approval</FieldLabel>
                <FieldDescription>
                  Turn off to let members send Inbox messages to Sales without waiting for a Team Leader.
                </FieldDescription>
              </FieldContent>
              <Switch
                id="inbox-approval"
                checked={s.inbox_requires_approval}
                disabled={save.isPending}
                onCheckedChange={(v) => save.mutate({ key: "inbox_requires_approval", value: v })}
              />
            </Field>
          ) : (
            <Skeleton className="h-20 w-full" />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <GlobeIcon className="size-4 text-primary" /> Timezone
          </CardTitle>
          <CardDescription>Used for dates, deadlines and overdue checks.</CardDescription>
        </CardHeader>
        <CardContent>
          {s ? (
            <Field className="max-w-sm">
              <FieldLabel>Organisation timezone</FieldLabel>
              <Select value={s.org_timezone} onValueChange={(v) => save.mutate({ key: "org_timezone", value: v })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent className="max-h-72">
                  {TIMEZONES.map((tz) => (
                    <SelectItem key={tz} value={tz}>
                      {tz.replaceAll("_", " ")}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : (
            <Skeleton className="h-9 w-80 max-w-full" />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BellIcon className="size-4 text-primary" /> Notifications
          </CardTitle>
          <CardDescription>
            When Sales makes several edits in a row, the team gets one combined notification instead of many.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {s ? (
            <BatchWindow
              key={s.batch_window_minutes}
              value={s.batch_window_minutes}
              pending={save.isPending}
              onSave={(v) => save.mutate({ key: "batch_window_minutes", value: v })}
            />
          ) : (
            <Skeleton className="h-9 w-64" />
          )}
        </CardContent>
      </Card>

      <SheetBackup />
    </div>
  );
}

function BatchWindow({ value, pending, onSave }: { value: number; pending: boolean; onSave: (v: number) => void }) {
  const [draft, setDraft] = useState(String(value));
  const n = Number(draft);
  const valid = Number.isInteger(n) && n >= 1 && n <= 120;
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (valid) onSave(n);
      }}
    >
      <Field data-invalid={!valid}>
        <FieldLabel htmlFor="batch">Batch window</FieldLabel>
        <div className="flex gap-2">
          <InputGroup className="w-40">
            <InputGroupInput id="batch" type="number" min={1} max={120} value={draft} onChange={(e) => setDraft(e.target.value)} />
            <InputGroupAddon align="inline-end">
              <InputGroupText>minutes</InputGroupText>
            </InputGroupAddon>
          </InputGroup>
          {n !== value && (
            <Button type="submit" disabled={!valid || pending}>
              Save
            </Button>
          )}
        </div>
        <FieldDescription>Between 1 and 120 minutes.</FieldDescription>
      </Field>
    </form>
  );
}
