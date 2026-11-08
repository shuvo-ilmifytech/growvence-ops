"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import {
  CheckCheckIcon,
  CheckIcon,
  DownloadIcon,
  FileIcon,
  FileTextIcon,
  ImageIcon,
  PaperclipIcon,
  PhoneIncomingIcon,
  PhoneMissedIcon,
  PhoneOffIcon,
  PhoneOutgoingIcon,
  SendHorizontalIcon,
  UploadCloudIcon,
  XIcon,
} from "lucide-react";
import { toast } from "sonner";
import { attachmentUrl, uploadChatFile, type ChatAttachment, type ChatCall, type ChatMessage, type OutgoingMessage } from "@/lib/chat";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";
import { PersonAvatar } from "@/components/messages/person-avatar";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";

const RUN_GAP_MS = 5 * 60 * 1000;
const MAX_FILES = 10;

const timeFmt = new Intl.DateTimeFormat("en", { hour: "numeric", minute: "2-digit" });
const dayFmt = new Intl.DateTimeFormat("en", { weekday: "long", day: "numeric", month: "long" });

// "Today" is the viewer's own day: read through a cached snapshot so it never
// lands in the prerender, and so React is never handed a fresh value per read.
let todayValue = "";
const noSubscription = () => () => {};
function readToday() {
  todayValue = new Date().toDateString();
  return todayValue;
}
function useToday() {
  return useSyncExternalStore(noSubscription, readToday, () => "");
}

function dayLabel(iso: string, today: string) {
  const day = new Date(iso).toDateString();
  if (today) {
    if (day === today) return "Today";
    const yesterday = new Date(today);
    yesterday.setDate(yesterday.getDate() - 1);
    if (day === yesterday.toDateString()) return "Yesterday";
  }
  return dayFmt.format(new Date(iso));
}

type Entry = { kind: "message"; at: string; message: ChatMessage } | { kind: "call"; at: string; call: ChatCall };

/** Messages and calls on one timeline, each message marked with where its run starts and ends. */
function timeline(messages: ChatMessage[], calls: ChatCall[]) {
  const entries: Entry[] = [
    ...messages.map((m) => ({ kind: "message" as const, at: m.created_at, message: m })),
    ...calls.map((c) => ({ kind: "call" as const, at: c.started_at, call: c })),
  ].sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime());
  const sameRun = (a: Entry | undefined, b: Entry | undefined) =>
    !!a &&
    !!b &&
    a.kind === "message" &&
    b.kind === "message" &&
    a.message.sender_id === b.message.sender_id &&
    new Date(a.at).toDateString() === new Date(b.at).toDateString() &&
    Math.abs(new Date(b.at).getTime() - new Date(a.at).getTime()) <= RUN_GAP_MS;
  return entries.map((e, i) => {
    const prev = entries[i - 1];
    const newDay = !prev || new Date(prev.at).toDateString() !== new Date(e.at).toDateString();
    return { entry: e, newDay, first: !sameRun(prev, e), last: !sameRun(e, entries[i + 1]) };
  });
}

/**
 * The conversation, shared by one-to-one and group threads: messages and (one-to-one)
 * finished calls in time order. `showSenders` names and pictures who is speaking,
 * which only a room with more than two people needs.
 */
export function MessageList({
  messages,
  calls = [],
  pending,
  empty,
  showSenders = false,
}: {
  messages: ChatMessage[];
  calls?: ChatCall[];
  pending: boolean;
  empty: React.ReactNode;
  showSenders?: boolean;
}) {
  const rows = useMemo(() => timeline(messages, calls), [messages, calls]);
  const today = useToday();
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  // Whether the reader is at the bottom; while they are, new messages and images that
  // finish loading keep the newest entry in view. Scrolling up to read stops that.
  const pinned = useRef(true);

  useEffect(() => {
    const box = scroller.current;
    const inner = content.current;
    if (!box || !inner) return;
    const toBottom = () => {
      if (pinned.current) box.scrollTop = box.scrollHeight;
    };
    const onScroll = () => {
      pinned.current = box.scrollHeight - box.scrollTop - box.clientHeight < 80;
    };
    toBottom();
    box.addEventListener("scroll", onScroll, { passive: true });
    const grow = new ResizeObserver(toBottom);
    grow.observe(inner);
    return () => {
      box.removeEventListener("scroll", onScroll);
      grow.disconnect();
    };
  }, [pending]);

  // A message of your own always brings you back down to it.
  const newest = rows[rows.length - 1]?.entry;
  const newestMine = newest?.kind === "message" && newest.message.mine ? newest.message.id : null;
  useEffect(() => {
    if (!newestMine || !scroller.current) return;
    pinned.current = true;
    scroller.current.scrollTop = scroller.current.scrollHeight;
  }, [newestMine]);

  return (
    <div ref={scroller} className="chat-wallpaper min-h-0 flex-1 overflow-y-auto px-3 py-4 md:px-6">
      <div ref={content}>
      {pending ? (
        <div className="mx-auto max-w-3xl space-y-3">
          <Skeleton className="h-10 w-2/5 rounded-2xl" />
          <Skeleton className="ml-auto h-14 w-1/2 rounded-2xl" />
          <Skeleton className="h-10 w-1/3 rounded-2xl" />
        </div>
      ) : rows.length === 0 ? (
        <div className="flex h-full items-center justify-center">{empty}</div>
      ) : (
        <div className="mx-auto max-w-3xl">
          {rows.map(({ entry, newDay, first, last }) => (
            <div key={entry.kind === "message" ? entry.message.id : `call-${entry.call.id}`}>
              {newDay && (
                <div className="sticky top-0 z-10 flex justify-center py-3">
                  <span className="rounded-full border bg-background/90 px-3 py-1 text-[11px] font-medium text-muted-foreground shadow-xs backdrop-blur">
                    {dayLabel(entry.at, today)}
                  </span>
                </div>
              )}
              {entry.kind === "call" ? (
                <CallRow call={entry.call} />
              ) : (
                <Bubble message={entry.message} first={first} last={last} showSender={showSenders} />
              )}
            </div>
          ))}
        </div>
      )}
      </div>
    </div>
  );
}

function Bubble({ message: m, first, last, showSender }: { message: ChatMessage; first: boolean; last: boolean; showSender: boolean }) {
  const images = m.attachments.filter((a) => a.mime_type.startsWith("image/"));
  const files = m.attachments.filter((a) => !a.mime_type.startsWith("image/"));
  const hasText = m.body.trim() !== "";
  const avatarSlot = showSender && !m.mine;

  return (
    <div className={cn("flex items-end gap-2", m.mine ? "justify-end" : "justify-start", first ? "mt-3" : "mt-0.5")}>
      {avatarSlot && (last ? <PersonAvatar name={m.sender_name} className="size-7" /> : <span className="w-7 shrink-0" />)}
      <div className={cn("flex max-w-[85%] flex-col gap-1 sm:max-w-[70%]", m.mine ? "items-end" : "items-start")}>
        {showSender && first && !m.mine && <span className="px-3 text-[11px] font-medium text-muted-foreground">{m.sender_name}</span>}
        {images.length > 0 && <ImageGrid images={images} />}
        {(hasText || files.length > 0) && (
          <div
            className={cn(
              "rounded-2xl px-3.5 py-2 text-[0.9rem] leading-relaxed shadow-xs",
              m.mine ? "bg-primary text-primary-foreground" : "border bg-card text-card-foreground",
              last && (m.mine ? "rounded-br-md" : "rounded-bl-md"),
            )}
          >
            {files.length > 0 && (
              <div className={cn("flex flex-col gap-1.5", hasText && "mb-2")}>
                {files.map((f) => (
                  <FileChip key={f.id} file={f} mine={m.mine} />
                ))}
              </div>
            )}
            {hasText && <p className="wrap-anywhere whitespace-pre-wrap">{m.body}</p>}
          </div>
        )}
        {last && (
          <span className="flex items-center gap-1 px-1 text-[11px] text-muted-foreground tabular-nums">
            {timeFmt.format(new Date(m.created_at))}
            {m.mine &&
              (m.read_at ? (
                <CheckCheckIcon className="size-3.5 text-sky-500" aria-label="Read" />
              ) : (
                <CheckIcon className="size-3.5" aria-label="Sent" />
              ))}
          </span>
        )}
      </div>
    </div>
  );
}

function ImageGrid({ images }: { images: ChatAttachment[] }) {
  return (
    <div className={cn("grid gap-1", images.length > 1 && "grid-cols-2")}>
      {images.map((img) => (
        <a
          key={img.id}
          href={attachmentUrl(img.id)}
          target="_blank"
          rel="noreferrer"
          className="block overflow-hidden rounded-2xl border bg-muted"
          title={`${img.name} · ${formatBytes(img.size)}`}
        >
          {/* eslint-disable-next-line @next/next/no-img-element -- presigned, short-lived URLs can't go through next/image */}
          <img
            src={attachmentUrl(img.id)}
            alt={img.name}
            loading="lazy"
            className={cn("object-cover transition-transform hover:scale-[1.02]", images.length > 1 ? "aspect-square w-36 sm:w-40" : "max-h-72 w-auto max-w-full")}
          />
        </a>
      ))}
    </div>
  );
}

function FileKindIcon({ mime }: { mime: string }) {
  if (mime.startsWith("image/")) return <ImageIcon className="size-4.5" />;
  if (mime.startsWith("text/") || mime.includes("pdf") || mime.includes("word") || mime.includes("document")) return <FileTextIcon className="size-4.5" />;
  return <FileIcon className="size-4.5" />;
}

function FileChip({ file, mine }: { file: ChatAttachment; mine: boolean }) {
  return (
    <a
      href={attachmentUrl(file.id)}
      className={cn(
        "flex min-w-48 items-center gap-3 rounded-xl px-2.5 py-2 transition-colors",
        mine ? "bg-primary-foreground/12 hover:bg-primary-foreground/20" : "bg-muted hover:bg-muted/70",
      )}
    >
      <span className={cn("flex size-9 shrink-0 items-center justify-center rounded-lg", mine ? "bg-primary-foreground/15" : "bg-background")}>
        <FileKindIcon mime={file.mime_type} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{file.name}</span>
        <span className={cn("block text-[11px]", mine ? "text-primary-foreground/70" : "text-muted-foreground")}>{formatBytes(file.size)}</span>
      </span>
      <DownloadIcon className="size-4 shrink-0 opacity-70" />
    </a>
  );
}

function duration(from: string, to: string) {
  const s = Math.max(0, Math.round((new Date(to).getTime() - new Date(from).getTime()) / 1000));
  const m = Math.floor(s / 60);
  const ss = String(s % 60).padStart(2, "0");
  return m >= 60 ? `${Math.floor(m / 60)}:${String(m % 60).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

/** A finished call, centred in the conversation like a note. */
function CallRow({ call: c }: { call: ChatCall }) {
  const talked = !!c.answered_at && !!c.ended_at;
  const missed = c.status === "missed" || c.status === "declined";
  const label = talked
    ? `${c.incoming ? "Incoming" : "Outgoing"} call`
    : c.status === "missed"
      ? c.incoming
        ? "Missed call"
        : "No answer"
      : c.status === "declined"
        ? c.incoming
          ? "You declined a call"
          : "Call declined"
        : "Call didn't connect";
  const Icon = talked ? (c.incoming ? PhoneIncomingIcon : PhoneOutgoingIcon) : missed ? PhoneMissedIcon : PhoneOffIcon;

  return (
    <div className="my-3 flex justify-center">
      <div className="flex items-center gap-2.5 rounded-full border bg-card px-3.5 py-1.5 text-xs shadow-xs">
        <span
          className={cn(
            "flex size-6 items-center justify-center rounded-full",
            talked ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" : "bg-rose-500/15 text-rose-600 dark:text-rose-400",
          )}
        >
          <Icon className="size-3.5" />
        </span>
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground tabular-nums">
          {talked ? `${duration(c.answered_at!, c.ended_at!)} · ` : ""}
          {timeFmt.format(new Date(c.started_at))}
        </span>
      </div>
    </div>
  );
}

// ── Composer ───────────────────────────────────────────────

type Pending = { key: string; file: File; preview: string | null; progress: number; uploadId?: string; error?: string; abort: AbortController };

/**
 * Write and send. Files upload as soon as they are picked (attach button, drag and
 * drop onto the conversation, or paste), so sending is instant. Enter sends;
 * Shift+Enter starts a new line.
 */
export function Composer({
  onSend,
  pending,
  disabled,
  dropTarget,
}: {
  /** `restore` puts the words back when sending fails. */
  onSend: (m: OutgoingMessage, restore: () => void) => void;
  pending: boolean;
  disabled?: boolean;
  /** The element files can be dropped on (the whole conversation). */
  dropTarget?: React.RefObject<HTMLElement | null>;
}) {
  const [draft, setDraft] = useState("");
  const [files, setFiles] = useState<Pending[]>([]);
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const count = useRef(0);
  useEffect(() => {
    count.current = files.length;
  }, [files.length]);

  const add = useCallback(
    (list: FileList | File[]) => {
      if (disabled) return;
      const room = MAX_FILES - count.current;
      const picked = Array.from(list);
      if (picked.length > room) toast.error(`Up to ${MAX_FILES} files per message`);
      const next = picked.slice(0, Math.max(0, room)).map(
        (file): Pending => ({
          key: crypto.randomUUID(),
          file,
          preview: file.type.startsWith("image/") ? URL.createObjectURL(file) : null,
          progress: 0,
          abort: new AbortController(),
        }),
      );
      if (next.length === 0) return;
      setFiles((cur) => [...cur, ...next]);
      for (const p of next) {
        const update = (patch: Partial<Pending>) => setFiles((all) => all.map((x) => (x.key === p.key ? { ...x, ...patch } : x)));
        uploadChatFile(p.file, (progress) => update({ progress }), p.abort.signal)
          .then((uploadId) => update({ uploadId, progress: 1 }))
          .catch((err: Error) => err.name !== "AbortError" && update({ error: err.message }));
      }
    },
    [disabled],
  );

  function remove(key: string) {
    const gone = files.find((x) => x.key === key);
    gone?.abort.abort();
    if (gone?.preview) URL.revokeObjectURL(gone.preview);
    setFiles((cur) => cur.filter((x) => x.key !== key));
  }

  // Dropping files anywhere on the conversation attaches them.
  useEffect(() => {
    const el = dropTarget?.current;
    if (!el || disabled) return;
    let depth = 0;
    const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes("Files");
    const enter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth++;
      setDragging(true);
    };
    const over = (e: DragEvent) => {
      if (hasFiles(e)) e.preventDefault();
    };
    const leave = () => {
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDragging(false);
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      setDragging(false);
      if (e.dataTransfer?.files.length) add(e.dataTransfer.files);
    };
    el.addEventListener("dragenter", enter);
    el.addEventListener("dragover", over);
    el.addEventListener("dragleave", leave);
    el.addEventListener("drop", drop);
    return () => {
      el.removeEventListener("dragenter", enter);
      el.removeEventListener("dragover", over);
      el.removeEventListener("dragleave", leave);
      el.removeEventListener("drop", drop);
    };
  }, [dropTarget, disabled, add]);

  // The box grows with what's written, up to a few lines.
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [draft]);

  const uploading = files.some((f) => !f.uploadId && !f.error);
  const failed = files.some((f) => f.error);
  const ready = (draft.trim() !== "" || files.length > 0) && !uploading && !failed;

  function submit() {
    if (!ready || pending || disabled) return;
    const body = draft.trim();
    const sent = files;
    setDraft("");
    setFiles([]);
    for (const f of sent) if (f.preview) URL.revokeObjectURL(f.preview);
    onSend({ body, attachments: sent.map((f) => f.uploadId!) }, () => {
      setDraft(body);
      if (sent.length) toast.error("Attach the files again: a failed send uses them up");
    });
  }

  return (
    <>
      {dragging && (
        <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center bg-background/80 backdrop-blur-sm">
          <div className="flex flex-col items-center gap-2 rounded-2xl border-2 border-dashed border-primary px-10 py-8 text-primary">
            <UploadCloudIcon className="size-8" />
            <span className="text-sm font-medium">Drop to attach</span>
          </div>
        </div>
      )}
      <form
        className="border-t bg-background/95 px-3 pt-2.5 pb-3 md:px-6"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <div className="mx-auto max-w-3xl">
          {files.length > 0 && (
            <div className="mb-2 flex gap-2 overflow-x-auto pb-1">
              {files.map((f) => (
                <div key={f.key} className="relative w-28 shrink-0 overflow-hidden rounded-xl border bg-muted/40">
                  {f.preview ? (
                    // eslint-disable-next-line @next/next/no-img-element -- a local blob preview
                    <img src={f.preview} alt="" className="h-20 w-full object-cover" />
                  ) : (
                    <div className="flex h-20 items-center justify-center">
                      <FileIcon className="size-7 text-muted-foreground" />
                    </div>
                  )}
                  <div className="px-2 py-1">
                    <p className="truncate text-[11px] font-medium" title={f.file.name}>
                      {f.file.name}
                    </p>
                    <p className={cn("truncate text-[10px]", f.error ? "text-destructive" : "text-muted-foreground")} title={f.error}>
                      {f.error ?? (f.uploadId ? formatBytes(f.file.size) : `${Math.round(f.progress * 100)}%`)}
                    </p>
                  </div>
                  {!f.uploadId && !f.error && (
                    <div className="absolute inset-x-0 top-0 h-0.5 bg-muted">
                      <div className="h-full bg-primary transition-[width]" style={{ width: `${f.progress * 100}%` }} />
                    </div>
                  )}
                  <button
                    type="button"
                    onClick={() => remove(f.key)}
                    aria-label={`Remove ${f.file.name}`}
                    className="absolute top-1 right-1 flex size-5 items-center justify-center rounded-full bg-background/90 shadow-xs hover:bg-background"
                  >
                    <XIcon className="size-3" />
                  </button>
                </div>
              ))}
            </div>
          )}
          <div className="flex items-end gap-1.5 rounded-2xl border bg-card p-1.5 shadow-xs transition-colors focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/30">
            <input
              ref={input}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                if (e.target.files?.length) add(e.target.files);
                e.target.value = "";
              }}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-9 shrink-0 rounded-xl text-muted-foreground"
              aria-label="Attach files"
              title="Attach files"
              disabled={disabled || files.length >= MAX_FILES}
              onClick={() => input.current?.click()}
            >
              <PaperclipIcon />
            </Button>
            <textarea
              ref={box}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  submit();
                }
              }}
              onPaste={(e) => {
                if (e.clipboardData.files.length) {
                  e.preventDefault();
                  add(e.clipboardData.files);
                }
              }}
              placeholder={disabled ? "This person no longer has access" : "Write a message…"}
              aria-label="Message"
              rows={1}
              disabled={disabled}
              className="max-h-40 min-h-9 flex-1 resize-none bg-transparent px-1 py-2 text-[0.9rem] outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
            />
            <Button type="submit" size="icon" className="size-9 shrink-0 rounded-xl" aria-label="Send" disabled={disabled || !ready || pending}>
              {pending || uploading ? <Spinner /> : <SendHorizontalIcon />}
            </Button>
          </div>
          <p className="mt-1.5 px-1 text-[11px] text-muted-foreground max-sm:hidden">
            Enter to send · Shift+Enter for a new line · drop or paste files to attach
          </p>
        </div>
      </form>
    </>
  );
}
