"use client";

import { useState } from "react";
import Link from "next/link";
import { PaperclipIcon, SendIcon, ShareIcon, UploadIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import {
  clientFileDownloadUrl,
  useAddClientMessage,
  useClientFiles,
  useClientMessages,
  useClients,
  useLinkClient,
  useProjectCollab,
  useShareProjectFile,
  useUnlinkClient,
  useUploadClientFile,
} from "@/lib/clients";
import { fileVersionDownloadUrl, useProjectFiles } from "@/lib/files";
import { formatBytes, formatDateTime, relativeTime } from "@/lib/format";
import { useMe } from "@/lib/session";

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
 * The Client tab on a project: link the business client, talk in the shared
 * message thread, and share files with them.
 */
export function ProjectClientTab({ projectId }: { projectId: string }) {
  const { data: me } = useMe();
  const collab = useProjectCollab(projectId);
  const clients = useClients();
  const link = useLinkClient(projectId);
  const unlink = useUnlinkClient(projectId);
  const [picked, setPicked] = useState("");

  const canManage = !!me && (me.is_admin || me.role === "sales" || me.role === "tl");
  const linked = collab.data?.clients ?? [];
  const linkedIds = new Set(linked.map((l) => l.client_id));
  const unlinked = (clients.data?.clients ?? []).filter((c) => !linkedIds.has(c.id) && c.status === "active");

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Linked clients</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {collab.isPending ? (
              <Skeleton className="h-16" />
            ) : linked.length === 0 ? (
              <p className="text-sm text-muted-foreground">No client linked yet. Link one to share tasks, messages and files with them.</p>
            ) : (
              <ul className="divide-y">
                {linked.map((l) => (
                  <li key={l.id} className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
                    <div className="min-w-0">
                      <Link href={`/clients/${l.client_id}`} className="font-medium hover:underline">
                        <span className="mr-2 font-mono text-xs text-muted-foreground">{l.client_code}</span>
                        {l.company_name}
                      </Link>
                      <p className="text-sm text-muted-foreground">
                        {[l.contact_person, l.email].filter(Boolean).join(" · ") || l.type}
                      </p>
                    </div>
                    {canManage && (
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Unlink ${l.company_name}`}
                        disabled={unlink.isPending}
                        onClick={() =>
                          unlink.mutate(l.client_id, {
                            onSuccess: () => toast.success("Client unlinked."),
                            onError: (err) => toast.error(err.message),
                          })
                        }
                      >
                        <XIcon />
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {canManage && (
              <div className="flex gap-2">
                <Select value={picked} onValueChange={setPicked}>
                  <SelectTrigger className="flex-1" aria-label="Pick a client to link">
                    <SelectValue placeholder="Pick a client to link…" />
                  </SelectTrigger>
                  <SelectContent>
                    {unlinked.length === 0 && <SelectItem value="" disabled>No unlinked clients</SelectItem>}
                    {unlinked.map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        {c.client_code} · {c.company_name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  disabled={!picked || link.isPending}
                  onClick={() =>
                    link.mutate(picked, {
                      onSuccess: () => (toast.success("Client linked."), setPicked("")),
                      onError: (err) => toast.error(err.message),
                    })
                  }
                >
                  {link.isPending && <Spinner />} Link
                </Button>
              </div>
            )}
            {canManage && (clients.data?.clients ?? []).length === 0 && (
              <p className="text-sm text-muted-foreground">
                No clients exist yet. <Link href="/clients" className="font-medium underline">Add one on the Clients page</Link> first.
              </p>
            )}
          </CardContent>
        </Card>

        <MessagesCard projectId={projectId} />
      </div>

      <div className="space-y-6">
        <FilesCard projectId={projectId} />
      </div>
    </div>
  );
}

function MessagesCard({ projectId }: { projectId: string }) {
  const messages = useClientMessages(projectId);
  const send = useAddClientMessage(projectId);
  const [draft, setDraft] = useState("");

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Messages with the client</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {messages.isPending ? (
          <Skeleton className="h-24" />
        ) : (messages.data?.messages ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No messages yet. Say hello — the linked client sees this thread.</p>
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
                    {m.author_role === "client" && <Badge className="ml-1.5 bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">Client</Badge>}
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
            placeholder="Message the client…"
            aria-label="Message the client"
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
  const files = useClientFiles(projectId);
  const projectFiles = useProjectFiles(projectId);
  const share = useShareProjectFile(projectId);
  const upload = useUploadClientFile(projectId);
  const [sharing, setSharing] = useState<string | null>(null);

  const sharedUrls = new Set((files.data?.files ?? []).filter((f) => !f.is_client_upload).map((f) => f.file_url));

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-base">Shared files</CardTitle>
        <Button asChild variant="outline" size="sm" disabled={upload.isPending}>
          <label className="cursor-pointer">
            {upload.isPending ? <Spinner /> : <UploadIcon />} Upload
            <input
              type="file"
              className="sr-only"
              onChange={async (e) => {
                const f = e.target.files?.[0];
                e.target.value = "";
                if (!f) return;
                try {
                  await upload.mutateAsync(f);
                  toast.success("File shared with the client.");
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : "Upload failed.");
                }
              }}
            />
          </label>
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        {(files.data?.files ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No files shared yet.</p>
        ) : (
          <ul className="divide-y rounded-lg border">
            {(files.data?.files ?? []).map((f) => (
              <li key={f.id} className="flex items-center gap-2 px-3 py-2 text-sm">
                <PaperclipIcon className="size-4 shrink-0 text-muted-foreground" />
                <a href={clientFileDownloadUrl(f.id)} className="min-w-0 flex-1 truncate font-medium hover:underline">
                  {f.file_name}
                </a>
                {f.is_client_upload && <Badge className="bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">From client</Badge>}
                <span className="hidden text-xs text-muted-foreground sm:inline">
                  {formatBytes(f.file_size)} · {f.uploaded_by_name ?? "—"}
                </span>
              </li>
            ))}
          </ul>
        )}

        <div className="space-y-2 border-t pt-4">
          <h4 className="text-sm font-medium">Share from project files</h4>
          {projectFiles.isPending ? (
            <Skeleton className="h-10" />
          ) : (projectFiles.data?.files ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">No project files to share yet.</p>
          ) : (
            <ul className="space-y-1">
              {(projectFiles.data?.files ?? []).map((pf) => {
                const url = fileVersionDownloadUrl(pf.version_id);
                const already = sharedUrls.has(url);
                return (
                  <li key={pf.id} className="flex items-center gap-2 text-sm">
                    <PaperclipIcon className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">{pf.original_name}</span>
                    {already ? (
                      <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">Shared</Badge>
                    ) : (
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={share.isPending && sharing === pf.id}
                        onClick={() => {
                          setSharing(pf.id);
                          share.mutate(
                            { file_name: pf.original_name, file_url: url, file_size: pf.size_bytes },
                            {
                              onSuccess: () => (toast.success("File shared with the client."), setSharing(null)),
                              onError: (err) => (toast.error(err.message), setSharing(null)),
                            },
                          );
                        }}
                      >
                        <ShareIcon className="size-4" /> Share
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
