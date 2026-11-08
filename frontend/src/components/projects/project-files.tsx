"use client";

import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  ChevronDownIcon,
  DownloadIcon,
  FileArchiveIcon,
  FileIcon,
  FileImageIcon,
  FileSpreadsheetIcon,
  FileTextIcon,
  HistoryIcon,
  MoreHorizontalIcon,
  PaperclipIcon,
  RefreshCwIcon,
  Trash2Icon,
  UploadCloudIcon,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api";
import { formatBytes, formatDateTime, relativeTime } from "@/lib/format";
import { useRemoveFile } from "@/lib/projects";
import { useMe } from "@/lib/session";
import { uploadProjectFile } from "@/lib/uploads";
import type { FileVersion, ProjectDetail, ProjectFile } from "@/lib/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

type Pending = { key: string; name: string; progress: number; error?: string };

function iconFor(mime: string, name: string) {
  if (mime.startsWith("image/")) return FileImageIcon;
  if (/zip|rar|7z|tar|gzip/.test(mime) || /\.(zip|rar|7z|tgz|gz)$/i.test(name)) return FileArchiveIcon;
  if (/sheet|excel|csv/.test(mime) || /\.(xlsx?|csv)$/i.test(name)) return FileSpreadsheetIcon;
  if (/pdf|word|text|document/.test(mime)) return FileTextIcon;
  return FileIcon;
}

function errorText(err: unknown) {
  if (err instanceof ApiError) return Object.values(err.fields)[0] ? `File ${Object.values(err.fields)[0]}` : err.message;
  return err instanceof Error ? err.message : "Upload failed";
}

export function ProjectFiles({ detail }: { detail: ProjectDetail }) {
  const p = detail.project;
  const qc = useQueryClient();
  const canUpload = detail.permissions.can_upload;
  const { data: me } = useMe();
  // Sales can remove any file; the people working on it only the files they added.
  const canRemove = (f: ProjectFile) => canUpload && (detail.permissions.can_remove_files || f.uploaded_by === me?.id);
  const inputRef = useRef<HTMLInputElement>(null);
  const replaceRef = useRef<HTMLInputElement>(null);
  const [replacing, setReplacing] = useState<ProjectFile | null>(null);
  const [pending, setPending] = useState<Pending[]>([]);
  const [dragging, setDragging] = useState(false);
  const [removing, setRemoving] = useState<ProjectFile | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const remove = useRemoveFile(p.id);

  const versionsByFile = new Map<string, FileVersion[]>();
  for (const v of detail.versions) versionsByFile.set(v.file_id, [...(versionsByFile.get(v.file_id) ?? []), v]);

  async function upload(files: File[], replace?: ProjectFile) {
    for (const file of files) {
      const key = `${file.name}-${Date.now()}-${Math.random()}`;
      setPending((ps) => [...ps, { key, name: file.name, progress: 0 }]);
      try {
        const d = await uploadProjectFile(p.id, file, {
          replaceFileId: replace?.id,
          onProgress: (f) => setPending((ps) => ps.map((x) => (x.key === key ? { ...x, progress: f } : x))),
        });
        qc.setQueryData(["projects", "detail", p.id], d);
        qc.invalidateQueries({ queryKey: ["projects", "list"] });
        setPending((ps) => ps.filter((x) => x.key !== key));
        toast.success(replace ? `Uploaded a new version of ${replace.original_name}` : `Uploaded ${file.name}`);
      } catch (err) {
        setPending((ps) => ps.map((x) => (x.key === key ? { ...x, error: errorText(err) } : x)));
      }
    }
  }

  const total = detail.files.length;

  return (
    <Card
      onDragOver={(e) => {
        if (!canUpload) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        if (!canUpload) return;
        e.preventDefault();
        setDragging(false);
        upload(Array.from(e.dataTransfer.files));
      }}
      className={cn(dragging && "ring-2 ring-primary")}
    >
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PaperclipIcon className="size-4 text-muted-foreground" />
          Files
          {total > 0 && <span className="text-sm font-normal text-muted-foreground">{total}</span>}
        </CardTitle>
        {canUpload && (
          <CardAction>
            <Button variant="outline" size="sm" onClick={() => inputRef.current?.click()}>
              <UploadCloudIcon /> Upload
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="space-y-3">
        <input
          ref={inputRef}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            upload(Array.from(e.target.files ?? []));
            e.target.value = "";
          }}
        />
        <input
          ref={replaceRef}
          type="file"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f && replacing) upload([f], replacing);
            setReplacing(null);
            e.target.value = "";
          }}
        />

        {total === 0 && pending.length === 0 && (
          <button
            type="button"
            disabled={!canUpload}
            onClick={() => inputRef.current?.click()}
            className="flex w-full flex-col items-center gap-1 rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground enabled:hover:border-primary/50 enabled:hover:bg-accent/40"
          >
            <UploadCloudIcon className="size-5" />
            {canUpload ? "Drop files here or click to upload" : "No files attached"}
            {canUpload && <span className="text-xs">Up to 1 GB each. Replacing a file keeps the old version.</span>}
          </button>
        )}

        <ul className="space-y-2">
          {detail.files.map((f) => {
            const Icon = iconFor(f.mime_type, f.original_name);
            const versions = versionsByFile.get(f.id) ?? [];
            const expanded = open === f.id;
            return (
              <li key={f.id} className="rounded-lg border">
                <div className="flex items-center gap-3 p-2.5">
                  <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-accent text-accent-foreground">
                    <Icon className="size-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <a
                      href={`/api/files/versions/${f.version_id}/download`}
                      className="block truncate text-sm font-medium hover:underline"
                      title={f.original_name}
                    >
                      {f.original_name}
                    </a>
                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                      {f.version_count > 1 && (
                        <Badge variant="secondary" className="h-4 px-1.5 text-[10px]">
                          v{f.version_no}
                        </Badge>
                      )}
                      <span>{formatBytes(f.size_bytes)}</span>
                      <span>·</span>
                      <span title={formatDateTime(f.version_created_at)}>{relativeTime(f.version_created_at)}</span>
                    </div>
                  </div>
                  <Button variant="ghost" size="icon-sm" asChild>
                    <a href={`/api/files/versions/${f.version_id}/download`} aria-label={`Download ${f.original_name}`}>
                      <DownloadIcon />
                    </a>
                  </Button>
                  {(canUpload || f.version_count > 1) && (
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon-sm" aria-label={`More for ${f.original_name}`}>
                          <MoreHorizontalIcon />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-52">
                        {f.version_count > 1 && (
                          <DropdownMenuItem onSelect={() => setOpen(expanded ? null : f.id)}>
                            <HistoryIcon /> {expanded ? "Hide" : "Show"} versions
                          </DropdownMenuItem>
                        )}
                        {canUpload && (
                          <>
                            <DropdownMenuItem
                              onSelect={() => {
                                setReplacing(f);
                                setTimeout(() => replaceRef.current?.click());
                              }}
                            >
                              <RefreshCwIcon /> Upload new version
                            </DropdownMenuItem>
                            {canRemove(f) && (
                              <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem variant="destructive" onSelect={() => setRemoving(f)}>
                                  <Trash2Icon /> Remove
                                </DropdownMenuItem>
                              </>
                            )}
                          </>
                        )}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  )}
                </div>
                {f.version_count > 1 && (
                  <button
                    type="button"
                    onClick={() => setOpen(expanded ? null : f.id)}
                    className="flex w-full items-center gap-1 border-t px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted/50"
                  >
                    <ChevronDownIcon className={cn("size-3 transition", expanded && "rotate-180")} />
                    {f.version_count} versions
                  </button>
                )}
                {expanded && (
                  <ol className="space-y-1 border-t bg-muted/30 px-3 py-2 text-xs">
                    {versions.map((v) => (
                      <li key={v.id} className="flex items-center justify-between gap-2">
                        <span>
                          <span className="font-medium">v{v.version_no}</span>{" "}
                          <span className="text-muted-foreground">
                            · {formatBytes(v.size_bytes)} · {v.uploaded_by_name} · {formatDateTime(v.created_at)}
                          </span>
                        </span>
                        <a href={`/api/files/versions/${v.id}/download`} className="text-primary hover:underline">
                          Download
                        </a>
                      </li>
                    ))}
                  </ol>
                )}
              </li>
            );
          })}
          {pending.map((u) => (
            <li key={u.key} className="rounded-lg border p-2.5">
              <div className="flex items-center justify-between gap-2 text-sm">
                <span className="truncate">{u.name}</span>
                {u.error ? (
                  <button
                    className="text-xs text-muted-foreground hover:text-foreground"
                    onClick={() => setPending((ps) => ps.filter((x) => x.key !== u.key))}
                  >
                    Dismiss
                  </button>
                ) : (
                  <span className="text-xs tabular-nums text-muted-foreground">{Math.round(u.progress * 100)}%</span>
                )}
              </div>
              {u.error ? (
                <p className="mt-1 text-xs text-destructive">{u.error}</p>
              ) : (
                <div className="mt-2 h-1 overflow-hidden rounded-full bg-muted">
                  <div className="h-full bg-primary transition-all" style={{ width: `${u.progress * 100}%` }} />
                </div>
              )}
            </li>
          ))}
        </ul>
      </CardContent>

      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove {removing?.original_name}?</AlertDialogTitle>
            <AlertDialogDescription>
              It disappears from the project for everyone. The removal is recorded in the activity, and its versions are kept on record.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep file</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() =>
                removing &&
                remove.mutate(removing.id, {
                  onSuccess: () => toast.success(`Removed ${removing.original_name}`),
                  onError: (err) => toast.error(errorText(err)),
                })
              }
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}
