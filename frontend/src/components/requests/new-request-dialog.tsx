"use client";

import { useState } from "react";
import { PaperclipIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { ApiError, api } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { useRaiseRequest } from "@/lib/requests";
import { useTeams } from "@/lib/teams";
import type { Profile } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";

/**
 * Sales raise a client request for a client with no project in the portal: one on monthly
 * management, or an order delivered before the portal was live. They pick the profile it
 * will go out on, name the client, add the order ID if there still is one, and send it to
 * the team that should do the work.
 */
export function NewRequestDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onCreated?: (id: string) => void;
}) {
  const profileList = useQuery({
    queryKey: ["profiles"],
    queryFn: () => api<{ profiles: Profile[] }>("/profiles").then((r) => r.profiles),
  });
  const teams = useTeams();
  const raise = useRaiseRequest();
  const [files, setFiles] = useState<File[]>([]);
  const [profile, setProfile] = useState("");
  const [team, setTeam] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const profiles = (profileList.data ?? []).filter((p) => p.is_active);
  const activeTeams = (teams.data ?? []).filter((t) => t.is_active);

  function reset() {
    setErrors({});
    setFiles([]);
    setProfile("");
    setTeam("");
  }

  return (
    <Dialog open={open} onOpenChange={(o) => (onOpenChange(o), o || reset())}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            const d = new FormData(e.currentTarget);
            setErrors({});
            raise.mutate(
              {
                profile_id: profile,
                team_id: team,
                client_name: String(d.get("client_name")).trim(),
                order_id: String(d.get("order_id")).trim() || null,
                title: String(d.get("title")).trim(),
                details: String(d.get("details")).trim(),
                files,
              },
              {
                onSuccess: (res) => {
                  toast.success("Request sent to the team. Its Team Leader has been told.");
                  if (res.failed.length) toast.error(`Couldn't attach ${res.failed.join(", ")}. Open the request and try again.`);
                  onOpenChange(false);
                  reset();
                  onCreated?.(res.id);
                },
                onError: (err) => (err instanceof ApiError && Object.keys(err.fields).length ? setErrors(err.fields) : toast.error(err.message)),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>New client request</DialogTitle>
            <DialogDescription>
              For a client with no project in the portal — monthly management, or an order delivered before this portal was live.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field data-invalid={!!errors.profile_id || undefined}>
              <FieldLabel htmlFor="request-profile">Profile the update goes out on</FieldLabel>
              <Select value={profile} onValueChange={setProfile}>
                <SelectTrigger id="request-profile" className="w-full">
                  <SelectValue placeholder="Pick a profile" />
                </SelectTrigger>
                <SelectContent>
                  {profiles.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name} · {p.platform}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldError>{errors.profile_id && `Profile ${errors.profile_id}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.client_name || undefined}>
              <FieldLabel htmlFor="request-client">Client</FieldLabel>
              <Input id="request-client" name="client_name" required maxLength={200} placeholder="The client's name or username" />
              <FieldError>{errors.client_name && `Client ${errors.client_name}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.order_id || undefined}>
              <FieldLabel htmlFor="request-order">Order ID</FieldLabel>
              <Input id="request-order" name="order_id" maxLength={100} placeholder="Optional" className="sm:w-60" />
              <FieldDescription>Leave it empty if the order is gone or there never was one.</FieldDescription>
              <FieldError>{errors.order_id && `Order ID ${errors.order_id}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.title || undefined}>
              <FieldLabel htmlFor="request-title">What the client asked for</FieldLabel>
              <Input id="request-title" name="title" required maxLength={200} placeholder="e.g. Add a size chart to the product pages" />
              <FieldError>{errors.title && `Title ${errors.title}`}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="request-details">Details</FieldLabel>
              <Textarea id="request-details" name="details" rows={4} maxLength={20000} placeholder="Paste or summarise the client's message" />
            </Field>
            <Field data-invalid={!!errors.team_id || undefined}>
              <FieldLabel htmlFor="request-team">Team to do it</FieldLabel>
              <Select value={team} onValueChange={setTeam}>
                <SelectTrigger id="request-team" className="w-full">
                  <SelectValue placeholder="Pick the team" />
                </SelectTrigger>
                <SelectContent>
                  {activeTeams.map((t) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                      {t.lead_name ? ` · ${t.lead_name}` : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>There is no project type to route it by, so pick the team yourself.</FieldDescription>
              <FieldError>{errors.team_id && `Team ${errors.team_id}`}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="request-files">Files from the client</FieldLabel>
              <Input
                id="request-files"
                type="file"
                multiple
                onChange={(e) => {
                  // Read the list before clearing the input: the state updater runs later.
                  const picked = Array.from(e.target.files ?? []);
                  e.target.value = "";
                  setFiles((cur) => [...cur, ...picked]);
                }}
              />
              {files.length > 0 && (
                <ul className="space-y-1">
                  {files.map((f, i) => (
                    <li key={`${f.name}-${i}`} className="flex items-center gap-2 text-sm">
                      <PaperclipIcon className="size-3.5 text-muted-foreground" />
                      <span className="min-w-0 flex-1 truncate">{f.name}</span>
                      <span className="text-xs text-muted-foreground">{formatBytes(f.size)}</span>
                      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${f.name}`} onClick={() => setFiles((cur) => cur.filter((_, j) => j !== i))}>
                        <XIcon />
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={raise.isPending}>
              {raise.isPending && <Spinner />} {raise.isPending && files.length ? "Uploading…" : "Send to the team"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
