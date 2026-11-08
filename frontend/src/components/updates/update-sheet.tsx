"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  CheckIcon,
  CopyIcon,
  DownloadIcon,
  ExternalLinkIcon,
  PaperclipIcon,
  SendIcon,
  Trash2Icon,
  UndoIcon,
  UploadCloudIcon,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { formatBytes, formatDateTime, initials, relativeTime } from "@/lib/format";
import { uploadUpdateAttachment, useUpdate, useUpdateMutation } from "@/lib/updates";
import {
  DESTINATION_HINTS,
  DESTINATION_LABELS,
  DESTINATIONS,
  ROLE_LABELS,
  UPDATE_STATUS_LABELS,
  type Destination,
  type UpdateDetail,
} from "@/lib/types";
import { DestinationLabel, UpdateStatusBadge } from "@/components/update-badges";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

function errorText(err: unknown) {
  if (err instanceof ApiError) return Object.values(err.fields)[0] ?? err.message;
  return err instanceof Error ? err.message : "Something went wrong";
}

export function UpdateSheet({ updateId, onClose }: { updateId: string | null; onClose: () => void }) {
  const detail = useUpdate(updateId);
  return (
    <Sheet open={!!updateId} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="gap-0 overflow-y-auto p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl">
        {detail.isPending && (
          <div className="space-y-4 p-6">
            <SheetTitle className="sr-only">Loading update</SheetTitle>
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
        {detail.isError && (
          <SheetHeader className="p-6">
            <SheetTitle>Update not available</SheetTitle>
            <SheetDescription>It may have been sent back for changes, or you don&apos;t have access to it.</SheetDescription>
          </SheetHeader>
        )}
        {detail.data && <UpdateBody key={detail.data.update.id} detail={detail.data} />}
      </SheetContent>
    </Sheet>
  );
}

function UpdateBody({ detail }: { detail: UpdateDetail }) {
  const u = detail.update;
  const perms = detail.permissions;
  const qc = useQueryClient();
  const [message, setMessage] = useState(u.message);
  const [error, setError] = useState("");
  const [changesOpen, setChangesOpen] = useState(false);
  const [uploading, setUploading] = useState<{ name: string; progress: number } | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const path = `/updates/${u.id}`;
  const save = useUpdateMutation(u.id, (v: { message?: string; destination?: Destination }) =>
    api<UpdateDetail>(path, { method: "PATCH", body: { ...v, lock_version: u.lock_version } }),
  );
  const submit = useUpdateMutation(u.id, () => api<UpdateDetail>(`${path}/submit`, { method: "POST" }));
  const approve = useUpdateMutation(u.id, (v: { destination?: Destination }) =>
    api<UpdateDetail>(`${path}/approve`, { method: "POST", body: v }),
  );
  const requestChanges = useUpdateMutation(u.id, (comment: string) =>
    api<UpdateDetail>(`${path}/request-changes`, { method: "POST", body: { comment } }),
  );
  const sent = useUpdateMutation(u.id, () => api<UpdateDetail>(`${path}/sent`, { method: "POST" }));
  const removeAttachment = useUpdateMutation(u.id, (fileId: string) =>
    api<UpdateDetail>(`${path}/attachments/${fileId}`, { method: "DELETE" }),
  );

  const dirty = message !== u.message;
  const busy = save.isPending || submit.isPending || approve.isPending || sent.isPending;
  const editable = perms.can_edit;
  const onError = (err: unknown) => setError(errorText(err));

  async function saveThen(next?: () => void) {
    setError("");
    if (dirty) {
      try {
        await save.mutateAsync({ message });
      } catch (err) {
        onError(err);
        return;
      }
    }
    next?.();
  }

  async function upload(files: File[]) {
    for (const f of files) {
      setUploading({ name: f.name, progress: 0 });
      try {
        const d = await uploadUpdateAttachment(u.id, f, (p) => setUploading({ name: f.name, progress: p }));
        qc.setQueryData(["updates", "detail", u.id], d);
        qc.invalidateQueries({ queryKey: ["updates", "list"] });
      } catch (err) {
        toast.error(errorText(err));
      }
    }
    setUploading(null);
  }

  const statusNote =
    u.status === "approved" && editable
      ? "Approved. Editing the text or attachments sends it back to the Team Leader."
      : u.status === "changes_requested" && editable
        ? "The Team Leader asked for changes. See the Ops thread, edit, then resubmit."
        : u.status === "approved" && perms.can_mark_sent
          ? `Post this on the ${DESTINATION_LABELS[u.destination]} of the order, then mark it as sent.`
          : null;

  return (
    <>
      <SheetHeader className="space-y-2 border-b p-6 pr-12">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm text-muted-foreground">UPD-{u.number}</span>
          <UpdateStatusBadge status={u.status} />
          {u.destination === "delivery" && u.revision_number !== null && (
            <Badge variant="outline">{u.revision_number === 0 ? "Original delivery" : `Revision ${u.revision_number}`}</Badge>
          )}
          {u.auto_approved && <Badge variant="outline">Auto-approved</Badge>}
        </div>
        <SheetTitle className="text-lg">
          Update to {DESTINATION_LABELS[u.destination]}
          <span className="font-normal text-muted-foreground"> for {u.client_name ?? "client"}</span>
        </SheetTitle>
        <SheetDescription asChild>
          <Link
            href={u.client_request_id ? `/requests/${u.client_request_id}` : `/projects/${u.project_id}`}
            className="inline-flex items-center gap-1 hover:text-foreground"
          >
            {u.client_request_id ? `CR-${u.project_number} ${u.client_name}` : `#${u.project_number} ${u.project_name}`}
            {u.order_id ? ` · ${u.order_id}` : u.client_request_id ? " · no order ID" : ""} · {u.profile_name}
            <ExternalLinkIcon className="size-3" />
          </Link>
        </SheetDescription>
      </SheetHeader>

      <div className="space-y-6 p-6">
        {statusNote && <div className="rounded-lg border border-primary/30 bg-accent px-4 py-3 text-sm text-accent-foreground">{statusNote}</div>}

        {/* Message */}
        <section className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">Message</h3>
            {!editable && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => navigator.clipboard.writeText(u.message).then(() => toast.success("Message copied"))}
              >
                <CopyIcon /> Copy
              </Button>
            )}
          </div>
          {editable ? (
            <Textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={8}
              placeholder="Write the message exactly as the client should read it."
            />
          ) : (
            <p className="rounded-lg border bg-muted/30 p-4 text-sm leading-relaxed whitespace-pre-wrap">
              {u.message || <span className="text-muted-foreground">No message yet.</span>}
            </p>
          )}
        </section>

        {/* Destination */}
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel>Update to</FieldLabel>
            {editable || perms.can_set_destination ? (
              <Select
                value={u.destination}
                onValueChange={(v) =>
                  save.mutate(
                    { destination: v as Destination },
                    {
                      onSuccess: () => toast.success(`Destination set to ${DESTINATION_LABELS[v as Destination]}`),
                      onError,
                    },
                  )
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DESTINATIONS.map((d) => (
                    <SelectItem key={d} value={d}>
                      {DESTINATION_LABELS[d]}
                      <span className="text-xs text-muted-foreground"> · {DESTINATION_HINTS[d]}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <DestinationLabel destination={u.destination} />
            )}
          </Field>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm [&_dd]:min-w-0">
            <dt className="text-muted-foreground">Written by</dt>
            <dd>{u.author_name}</dd>
            <dt className="text-muted-foreground">Checked by</dt>
            <dd>{u.checked_by_name ? `${u.checked_by_name}, ${formatDateTime(u.checked_at!)}` : u.auto_approved ? "Auto-approved" : "—"}</dd>
            <dt className="text-muted-foreground">Sent by</dt>
            <dd>{u.sent_by_name ? `${u.sent_by_name}, ${formatDateTime(u.sent_at!)}` : "—"}</dd>
          </dl>
        </section>

        {/* Attachments */}
        <section className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="flex items-center gap-2 text-sm font-medium">
              <PaperclipIcon className="size-4 text-muted-foreground" /> Attachments
              {u.destination === "delivery" && <span className="text-xs font-normal text-muted-foreground">(at least one for Delivery)</span>}
            </h3>
            {editable && (
              <Button variant="outline" size="sm" onClick={() => fileRef.current?.click()} disabled={!!uploading}>
                <UploadCloudIcon /> Attach
              </Button>
            )}
            <input
              ref={fileRef}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                upload(Array.from(e.target.files ?? []));
                e.target.value = "";
              }}
            />
          </div>
          {detail.attachments.length === 0 && !uploading && <p className="text-sm text-muted-foreground">No attachments.</p>}
          <ul className="space-y-1.5">
            {detail.attachments.map((a) => (
              <li key={a.id} className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
                <a href={`/api/files/versions/${a.version_id}/download`} className="min-w-0 flex-1 truncate hover:underline">
                  {a.original_name}
                </a>
                <span className="text-xs text-muted-foreground">{formatBytes(a.size_bytes)}</span>
                <Button variant="ghost" size="icon-sm" asChild>
                  <a href={`/api/files/versions/${a.version_id}/download`} aria-label={`Download ${a.original_name}`}>
                    <DownloadIcon />
                  </a>
                </Button>
                {editable && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Remove ${a.original_name}`}
                    onClick={() => removeAttachment.mutate(a.id, { onError })}
                  >
                    <Trash2Icon />
                  </Button>
                )}
              </li>
            ))}
            {uploading && (
              <li className="rounded-md border px-3 py-2 text-sm">
                <div className="flex justify-between">
                  <span className="truncate">{uploading.name}</span>
                  <span className="text-xs text-muted-foreground">{Math.round(uploading.progress * 100)}%</span>
                </div>
                <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-muted">
                  <div className="h-full bg-primary" style={{ width: `${uploading.progress * 100}%` }} />
                </div>
              </li>
            )}
          </ul>
        </section>

        {error && <p className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</p>}

        {/* Actions */}
        <div className="flex flex-wrap gap-2">
          {editable && dirty && (
            <Button variant="outline" disabled={busy} onClick={() => saveThen(() => toast.success("Saved"))}>
              {save.isPending && <Spinner />} Save
            </Button>
          )}
          {perms.can_submit && (
            <Button
              disabled={busy}
              onClick={() =>
                saveThen(() =>
                  submit.mutate(undefined, {
                    onSuccess: (d) =>
                      toast.success(d.update.status === "approved" ? "Approved automatically. Sales can send it now." : "Sent to the Team Leader for approval"),
                    onError,
                  }),
                )
              }
            >
              {submit.isPending ? <Spinner /> : <SendIcon />}
              {u.status === "changes_requested" ? "Resubmit for approval" : detail.needs_approval ? "Submit for approval" : "Submit"}
            </Button>
          )}
          {perms.can_approve && (
            <Button
              disabled={busy}
              onClick={() => approve.mutate({}, { onSuccess: () => toast.success("Approved. Sales has been told it's ready."), onError })}
            >
              {approve.isPending ? <Spinner /> : <CheckIcon />} Approve
            </Button>
          )}
          {perms.can_request_changes && (
            <Button variant="outline" disabled={busy} onClick={() => setChangesOpen(true)}>
              <UndoIcon /> Request changes
            </Button>
          )}
          {perms.can_mark_sent && (
            <Button disabled={busy} onClick={() => sent.mutate(undefined, { onSuccess: () => toast.success("Marked as sent"), onError })}>
              {sent.isPending ? <Spinner /> : <SendIcon />} Mark as sent
            </Button>
          )}
        </div>

        <Separator />

        <Threads detail={detail} />

        {/* History */}
        <section className="space-y-2">
          <h3 className="text-sm font-medium">History</h3>
          <ol className="space-y-1.5 text-sm">
            {detail.history.map((h) => (
              <li key={h.id} className="flex flex-wrap gap-x-2">
                <span className="font-medium">{h.changed_by_name}</span>
                <span className="text-muted-foreground">
                  {h.from_status ? `${UPDATE_STATUS_LABELS[h.from_status]} → ` : "created as "}
                  {UPDATE_STATUS_LABELS[h.to_status]}
                </span>
                <span className="text-xs text-muted-foreground" title={formatDateTime(h.created_at)}>
                  {relativeTime(h.created_at)}
                </span>
                {h.comment && <span className="w-full text-muted-foreground italic">{h.comment}</span>}
              </li>
            ))}
          </ol>
        </section>
      </div>

      <RequestChangesDialog
        open={changesOpen}
        onOpenChange={setChangesOpen}
        onConfirm={(comment) =>
          requestChanges.mutateAsync(comment).then(() => {
            toast.success("Sent back to the member");
            setChangesOpen(false);
          })
        }
      />
    </>
  );
}

function Threads({ detail }: { detail: UpdateDetail }) {
  const perms = detail.permissions;
  const threads = (perms.can_read_ops ? ["ops", "sales"] : ["sales"]) as ("ops" | "sales")[];
  return (
    <Tabs defaultValue={threads[0]}>
      <TabsList>
        {threads.map((t) => (
          <TabsTrigger key={t} value={t}>
            {t === "ops" ? "Ops thread" : "Sales thread"}
            <span className="ml-1.5 text-xs text-muted-foreground">{detail.comments.filter((c) => c.thread === t).length}</span>
          </TabsTrigger>
        ))}
      </TabsList>
      {threads.map((t) => (
        <TabsContent key={t} value={t} className="space-y-3 pt-2">
          <p className="text-xs text-muted-foreground">
            {t === "ops" ? "Team Leader and team member only. Sales can't see this thread." : "Visible to Sales, the Team Leader and the team member."}
          </p>
          {detail.comments
            .filter((c) => c.thread === t)
            .map((c) => (
              <div key={c.id} className="flex gap-2.5">
                <Avatar className="size-7">
                  <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">{initials(c.author_name)}</AvatarFallback>
                </Avatar>
                <div className="min-w-0 flex-1 rounded-lg border bg-card px-3 py-2">
                  <p className="text-xs">
                    <span className="font-medium">{c.author_name}</span>{" "}
                    <span className="text-muted-foreground">
                      {ROLE_LABELS[c.author_role]} · {relativeTime(c.created_at)}
                    </span>
                  </p>
                  <p className="mt-0.5 text-sm whitespace-pre-wrap">{c.body}</p>
                </div>
              </div>
            ))}
          {(t === "ops" ? perms.can_comment_ops : perms.can_comment_sales) && <Composer updateId={detail.update.id} thread={t} />}
        </TabsContent>
      ))}
    </Tabs>
  );
}

function Composer({ updateId, thread }: { updateId: string; thread: "ops" | "sales" }) {
  const [body, setBody] = useState("");
  const post = useUpdateMutation(updateId, (b: string) =>
    api<UpdateDetail>(`/updates/${updateId}/comments`, { method: "POST", body: { thread, body: b } }),
  );
  return (
    <div className="flex gap-2">
      <Textarea
        rows={2}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        placeholder={thread === "ops" ? "Message the ops team…" : "Message Sales…"}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && body.trim()) post.mutate(body.trim(), { onSuccess: () => setBody("") });
        }}
      />
      <Button
        size="icon"
        className={cn("self-end")}
        disabled={!body.trim() || post.isPending}
        aria-label="Send comment"
        onClick={() => post.mutate(body.trim(), { onSuccess: () => setBody(""), onError: (e) => toast.error(e.message) })}
      >
        {post.isPending ? <Spinner /> : <SendIcon />}
      </Button>
    </div>
  );
}

function RequestChangesDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  onConfirm: (comment: string) => Promise<void>;
}) {
  const [comment, setComment] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-5"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!comment.trim()) return setError("Tell the member what to change.");
            setPending(true);
            try {
              await onConfirm(comment.trim());
              setComment("");
            } catch (err) {
              setError(errorText(err));
            } finally {
              setPending(false);
            }
          }}
        >
          <DialogHeader>
            <DialogTitle>Request changes</DialogTitle>
            <DialogDescription>This starts the Ops thread. Sales won&apos;t see it.</DialogDescription>
          </DialogHeader>
          <Field data-invalid={!!error || undefined}>
            <Textarea autoFocus rows={4} value={comment} onChange={(e) => setComment(e.target.value)} placeholder="What should change?" />
            <FieldError>{error}</FieldError>
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={pending}>
              {pending && <Spinner />} Send back
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
