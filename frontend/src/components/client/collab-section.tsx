"use client";

import { useState } from "react";
import { CheckIcon, ChevronDownIcon, MessageSquareIcon, PaperclipIcon, SendIcon, UploadIcon } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import {
  clientFileDownloadUrl,
  useAddClientMessage,
  useAddTaskComment,
  useClientCollab,
  useClientFiles,
  useClientMessages,
  useReviewApproval,
  useTaskComments,
  useUploadClientFile,
  type CollabCard,
} from "@/lib/clients";
import { formatBytes, formatDateTime, relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

const COLUMN_TONE: Record<string, string> = {
  todo: "bg-muted text-muted-foreground",
  in_progress: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  review: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
  done: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
};

const COLUMN_LABEL: Record<string, string> = {
  todo: "To do",
  in_progress: "In progress",
  review: "In review",
  done: "Done",
};

function initials(name: string | null) {
  return (name ?? "?")
    .split(" ")
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0])
    .join("")
    .toUpperCase();
}

/**
 * Collaboration on the client portal: the tasks our team shared with this
 * client (with comments + approval), the message thread and shared files.
 */
export function ClientCollabSection({ projectId }: { projectId: string }) {
  const collab = useClientCollab(projectId);
  const cards = collab.data?.cards ?? [];

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <CheckIcon className="size-4" /> Tasks shared with you
          </CardTitle>
        </CardHeader>
        <CardContent>
          {collab.isPending ? (
            <div className="space-y-3">
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
            </div>
          ) : cards.length === 0 ? (
            <p className="text-sm text-muted-foreground">Our team hasn&apos;t shared any tasks with you yet.</p>
          ) : (
            <ul className="divide-y">
              {cards.map((card) => (
                <CollabTaskRow key={card.id} projectId={projectId} card={card} />
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <MessagesCard projectId={projectId} />
      <FilesCard projectId={projectId} />
    </div>
  );
}

function CollabTaskRow({ projectId, card }: { projectId: string; card: CollabCard }) {
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState("");
  const [askingChanges, setAskingChanges] = useState(false);
  const review = useReviewApproval(projectId, card.id);
  const pending = card.approval_status === "pending";

  async function decide(status: "approved" | "changes_requested") {
    try {
      await review.mutateAsync({ status, note: note.trim() });
      toast.success(status === "approved" ? "Task approved. Thank you!" : "Change request sent to our team.");
      setNote("");
      setAskingChanges(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Couldn't send your decision.");
    }
  }

  return (
    <li className="py-3 first:pt-0 last:pb-0">
      <button type="button" onClick={() => setOpen(!open)} className="flex w-full flex-wrap items-center gap-3 text-left">
        <ChevronDownIcon className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} />
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium">{card.title}</span>
          <span className="text-xs text-muted-foreground">
            {card.due_date ? `Due ${new Date(card.due_date + "T00:00:00").toLocaleDateString()}` : "No due date"}
            {card.assignee_names.length > 0 && ` · ${card.assignee_names.join(", ")}`}
          </span>
        </span>
        <Badge className={cn(COLUMN_TONE[card.column_key] ?? "bg-muted")}>{COLUMN_LABEL[card.column_key] ?? card.column_key}</Badge>
        {pending && <Badge className="bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200">Needs your approval</Badge>}
        {card.approval_status === "approved" && <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">Approved</Badge>}
        {card.approval_status === "changes_requested" && <Badge className="bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200">Changes requested</Badge>}
      </button>

      {open && (
        <div className="mt-3 space-y-4 pl-8">
          {card.description && <p className="text-sm whitespace-pre-wrap text-muted-foreground">{card.description}</p>}
          {pending && (
            <div className="space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-900 dark:bg-amber-950">
              <p className="text-sm font-medium">Our team is asking for your approval on this task.</p>
              {askingChanges ? (
                <div className="grid gap-2">
                  <Textarea
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                    rows={2}
                    placeholder="Tell us what needs to change…"
                    maxLength={2000}
                  />
                  <div className="flex gap-2">
                    <Button size="sm" disabled={review.isPending || !note.trim()} onClick={() => decide("changes_requested")}>
                      {review.isPending && <Spinner />} Send change request
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => setAskingChanges(false)}>
                      Cancel
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="flex gap-2">
                  <Button size="sm" disabled={review.isPending} onClick={() => decide("approved")}>
                    <CheckIcon className="size-4" /> Approve
                  </Button>
                  <Button size="sm" variant="outline" disabled={review.isPending} onClick={() => setAskingChanges(true)}>
                    Request changes
                  </Button>
                </div>
              )}
            </div>
          )}
          <TaskComments projectId={projectId} cardId={card.id} />
        </div>
      )}
    </li>
  );
}

function TaskComments({ projectId, cardId }: { projectId: string; cardId: string }) {
  const comments = useTaskComments(projectId, cardId, true);
  const add = useAddTaskComment(projectId, cardId, true);
  const [draft, setDraft] = useState("");

  return (
    <div className="space-y-3">
      <h5 className="text-sm font-medium">Comments</h5>
      {comments.isPending ? (
        <Skeleton className="h-12" />
      ) : (comments.data?.comments ?? []).length === 0 ? (
        <p className="text-sm text-muted-foreground">No comments yet.</p>
      ) : (
        <ul className="space-y-3">
          {(comments.data?.comments ?? []).map((m) => (
            <li key={m.id} className="flex gap-2">
              <Avatar className="size-6">
                <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">{initials(m.author_name)}</AvatarFallback>
              </Avatar>
              <div className="min-w-0">
                <div className="text-xs text-muted-foreground">
                  <span className="font-medium text-foreground">{m.author_name ?? "Someone"}</span>
                  {" · "}
                  <span title={formatDateTime(m.created_at)}>{relativeTime(m.created_at)}</span>
                </div>
                <p className="text-sm whitespace-pre-wrap">{m.body}</p>
              </div>
            </li>
          ))}
        </ul>
      )}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!draft.trim()) return;
          add.mutate(
            { body: draft.trim() },
            { onSuccess: () => setDraft(""), onError: (err) => toast.error(err.message) },
          );
        }}
      >
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={2}
          placeholder="Comment on this task…"
          aria-label="Comment on this task"
          maxLength={5000}
        />
        <Button type="submit" variant="outline" className="self-end" disabled={add.isPending || !draft.trim()}>
          <SendIcon className="size-4" /> Send
        </Button>
      </form>
    </div>
  );
}

function MessagesCard({ projectId }: { projectId: string }) {
  const messages = useClientMessages(projectId, true);
  const send = useAddClientMessage(projectId, true);
  const [draft, setDraft] = useState("");

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <MessageSquareIcon className="size-4" /> Messages with our team
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {messages.isPending ? (
          <Skeleton className="h-24" />
        ) : (messages.data?.messages ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No messages yet.</p>
        ) : (
          <ul className="max-h-96 space-y-3 overflow-y-auto">
            {(messages.data?.messages ?? []).map((m) => (
              <li key={m.id} className="flex gap-2">
                <Avatar className="size-6">
                  <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">{initials(m.author_name)}</AvatarFallback>
                </Avatar>
                <div className="min-w-0">
                  <div className="text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">{m.author_name ?? "Someone"}</span>
                    {" · "}
                    <span title={formatDateTime(m.created_at)}>{relativeTime(m.created_at)}</span>
                  </div>
                  <p className="text-sm whitespace-pre-wrap">{m.body}</p>
                </div>
              </li>
            ))}
          </ul>
        )}
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!draft.trim()) return;
            send.mutate(draft.trim(), {
              onSuccess: () => setDraft(""),
              onError: (err) => toast.error(err.message),
            });
          }}
        >
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={2}
            placeholder="Message our team…"
            aria-label="Message our team"
            maxLength={5000}
          />
          <Button type="submit" variant="outline" className="self-end" disabled={send.isPending || !draft.trim()}>
            <SendIcon className="size-4" /> Send
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function FilesCard({ projectId }: { projectId: string }) {
  const files = useClientFiles(projectId, true);
  const upload = useUploadClientFile(projectId, true);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2 text-base">
          <PaperclipIcon className="size-4" /> Shared files
        </CardTitle>
        <Button asChild variant="outline" size="sm" disabled={upload.isPending}>
          <label className="cursor-pointer">
            {upload.isPending ? <Spinner /> : <UploadIcon />} Upload a file
            <input
              type="file"
              className="sr-only"
              onChange={async (e) => {
                const f = e.target.files?.[0];
                e.target.value = "";
                if (!f) return;
                try {
                  await upload.mutateAsync(f);
                  toast.success("File uploaded.");
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : "Upload failed.");
                }
              }}
            />
          </label>
        </Button>
      </CardHeader>
      <CardContent>
        {files.isPending ? (
          <Skeleton className="h-16" />
        ) : (files.data?.files ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No files shared yet.</p>
        ) : (
          <ul className="divide-y rounded-lg border">
            {(files.data?.files ?? []).map((f) => (
              <li key={f.id} className="flex items-center gap-2 px-3 py-2 text-sm">
                <PaperclipIcon className="size-4 shrink-0 text-muted-foreground" />
                <a href={clientFileDownloadUrl(f.id)} className="min-w-0 flex-1 truncate font-medium hover:underline">
                  {f.file_name}
                </a>
                {f.is_client_upload && <Badge className="bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">You uploaded</Badge>}
                <span className="hidden text-xs text-muted-foreground sm:inline">
                  {formatBytes(f.file_size)} · {f.uploaded_by_name ?? "—"}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
