"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { MessageCircleIcon, PaperclipIcon, SearchIcon, XIcon } from "lucide-react";
import { useGroups, useInbox } from "@/lib/chat";
import { useDirectory } from "@/lib/hr";
import { useHydrated, useMe } from "@/lib/session";
import { relativeTime } from "@/lib/format";
import { ROLE_LABELS, type Role } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Conversation } from "@/components/messages/conversation";
import { GroupConversation } from "@/components/messages/group-conversation";
import { NewGroupDialog } from "@/components/messages/new-group-dialog";
import { PersonAvatar } from "@/components/messages/person-avatar";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "@/components/ui/input-group";
import { Skeleton } from "@/components/ui/skeleton";

export default function MessagesPage() {
  return (
    <Suspense fallback={<Skeleton className="h-[calc(100svh-7.5rem)] rounded-2xl" />}>
      <Messages />
    </Suspense>
  );
}

/** One row in the list: a person or a group, whichever spoke last. */
type Row = {
  key: string;
  href: string;
  name: string;
  group: boolean;
  at: string;
  who: string; // "You: ", "Sam: " or nothing
  preview: string;
  file: boolean; // the last message was only a file
  unread: number;
};

const FILTERS = [
  { key: "all", label: "All" },
  { key: "unread", label: "Unread" },
  { key: "people", label: "People" },
  { key: "groups", label: "Groups" },
] as const;
type Filter = (typeof FILTERS)[number]["key"];

function Messages() {
  const sp = useSearchParams();
  const router = useRouter();
  const withUser = sp.get("with");
  const withGroup = sp.get("group");
  const open = withGroup ?? withUser;
  const inbox = useInbox();
  const groups = useGroups();
  const { data: me } = useMe();
  const [find, setFind] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const directory = useDirectory();
  // Conversations are nobody's business until the page is the viewer's own: the
  // prerendered HTML has none, so showing cached ones before hydration mismatches.
  const loading = !useHydrated() || inbox.isPending || groups.isPending;

  const threads = useMemo(() => inbox.data?.threads ?? [], [inbox.data]);
  const rooms = useMemo(() => groups.data ?? [], [groups.data]);
  const unreadOf = withGroup
    ? (rooms.find((g) => g.id === withGroup)?.unread ?? 0)
    : (threads.find((t) => t.user_id === withUser)?.unread ?? 0);
  const query = find.trim().toLowerCase();

  // People and groups share one list, newest conversation first.
  const all = useMemo(() => {
    const out: Row[] = [
      ...rooms.map((g) => ({
        key: g.id,
        href: `/messages?group=${g.id}`,
        name: g.name,
        group: true,
        at: g.last_at ?? g.created_at,
        who: g.last_at ? (g.last_from_me ? "You: " : g.last_sender_name ? `${g.last_sender_name.split(" ")[0]}: ` : "") : "",
        preview: g.last_at ? (g.last_body ?? "") : "No messages yet",
        file: !!g.last_at && !g.last_body?.trim(),
        unread: g.unread,
      })),
      ...threads.map((t) => ({
        key: t.user_id,
        href: `/messages?with=${t.user_id}`,
        name: t.user_name,
        group: false,
        at: t.last_at,
        who: t.last_from_me ? "You: " : "",
        preview: t.last_body,
        file: !t.last_body.trim(),
        unread: t.unread,
      })),
    ];
    return out.sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime());
  }, [rooms, threads]);

  const rows = all.filter(
    (r) =>
      (!query || r.name.toLowerCase().includes(query)) &&
      (filter === "all" || (filter === "unread" ? r.unread > 0 : filter === "groups" ? r.group : !r.group)),
  );
  // Counted only once the list is the viewer's own, like the list itself (see `loading`).
  const totalUnread = loading ? 0 : all.reduce((n, r) => n + (r.unread > 0 ? 1 : 0), 0);

  // Anyone in the organisation can be started with, not only people already talked to.
  const people = !query
    ? []
    : (directory.data ?? [])
        .filter((p) => p.user_id !== me?.id && p.is_active && p.name.toLowerCase().includes(query) && !threads.some((t) => t.user_id === p.user_id))
        .slice(0, 6);

  // The page is a messenger: one panel that fills the window, list on the left and
  // the open conversation on the right (one at a time on a phone).
  return (
    <div className="flex h-[calc(100svh-5.5rem)] min-h-[30rem] overflow-hidden rounded-2xl border bg-background shadow-sm md:h-[calc(100svh-7.5rem)]">
      <aside className={cn("flex min-h-0 w-full flex-col border-r bg-muted/20 lg:w-[22rem] lg:shrink-0", open && "max-lg:hidden")}>
        <div className="space-y-3 border-b px-4 pt-4 pb-3">
          <div className="flex items-center justify-between gap-2">
            <h1 className="text-xl font-semibold tracking-tight">Messages</h1>
            <NewGroupDialog />
          </div>
          <InputGroup className="rounded-full bg-background">
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput value={find} onChange={(e) => setFind(e.target.value)} placeholder="Search or start a chat" aria-label="Find a person" />
            {find && (
              <InputGroupAddon align="inline-end">
                <InputGroupButton size="icon-xs" aria-label="Clear" onClick={() => setFind("")}>
                  <XIcon />
                </InputGroupButton>
              </InputGroupAddon>
            )}
          </InputGroup>
          <div className="flex gap-1.5 overflow-x-auto" role="tablist" aria-label="Show">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                type="button"
                role="tab"
                aria-selected={filter === f.key}
                onClick={() => setFilter(f.key)}
                className={cn(
                  "flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-colors",
                  filter === f.key ? "bg-primary text-primary-foreground" : "bg-background text-muted-foreground ring-1 ring-border hover:text-foreground",
                )}
              >
                {f.label}
                {f.key === "unread" && totalUnread > 0 && (
                  <span
                    className={cn(
                      "rounded-full px-1.5 text-[10px] tabular-nums",
                      filter === f.key ? "bg-primary-foreground/20" : "bg-primary text-primary-foreground",
                    )}
                  >
                    {totalUnread}
                  </span>
                )}
              </button>
            ))}
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {loading ? (
            <div className="space-y-1">
              {Array.from({ length: 6 }, (_, i) => (
                <div key={i} className="flex items-center gap-3 p-2">
                  <Skeleton className="size-11 rounded-full" />
                  <div className="flex-1 space-y-1.5">
                    <Skeleton className="h-3.5 w-1/2" />
                    <Skeleton className="h-3 w-3/4" />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <>
              {rows.length > 0 && (
                <ul className="space-y-0.5">
                  {rows.map((r) => {
                    const active = r.key === open;
                    return (
                      <li key={r.key}>
                        <button
                          type="button"
                          aria-current={active}
                          className={cn(
                            "flex w-full items-center gap-3 rounded-xl px-2.5 py-2.5 text-left transition-colors",
                            active ? "bg-primary/10" : "hover:bg-muted",
                          )}
                          onClick={() => router.push(r.href)}
                        >
                          <PersonAvatar name={r.name} group={r.group} className="size-11" />
                          <span className="min-w-0 flex-1">
                            <span className="flex items-baseline gap-2">
                              <span className={cn("min-w-0 flex-1 truncate text-sm", r.unread > 0 ? "font-semibold" : "font-medium")}>{r.name}</span>
                              <span className={cn("shrink-0 text-[11px]", r.unread > 0 ? "font-medium text-primary" : "text-muted-foreground")}>
                                {relativeTime(r.at)}
                              </span>
                            </span>
                            <span className="mt-0.5 flex items-center gap-2">
                              <span
                                className={cn(
                                  "flex min-w-0 flex-1 items-center gap-1 truncate text-xs",
                                  r.unread > 0 ? "text-foreground" : "text-muted-foreground",
                                )}
                              >
                                {r.who && <span className="shrink-0">{r.who}</span>}
                                {r.file ? (
                                  <>
                                    <PaperclipIcon className="size-3 shrink-0" />
                                    <span className="truncate">Sent a file</span>
                                  </>
                                ) : (
                                  <span className="truncate">{r.preview}</span>
                                )}
                              </span>
                              {r.unread > 0 && (
                                <span className="flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-primary px-1.5 text-[11px] font-semibold text-primary-foreground tabular-nums">
                                  {r.unread}
                                </span>
                              )}
                            </span>
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}

              {people.length > 0 && (
                <>
                  <p className="px-2.5 pt-4 pb-1.5 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">Start a new chat</p>
                  <ul className="space-y-0.5">
                    {people.map((p) => (
                      <li key={p.user_id}>
                        <button
                          type="button"
                          className="flex w-full items-center gap-3 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted"
                          onClick={() => {
                            setFind("");
                            router.push(`/messages?with=${p.user_id}`);
                          }}
                        >
                          <PersonAvatar name={p.name} className="size-9" />
                          <span className="min-w-0 flex-1 truncate font-medium">{p.name}</span>
                          <span className="shrink-0 text-xs text-muted-foreground">{ROLE_LABELS[p.role as Role] ?? p.role}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </>
              )}

              {rows.length === 0 && people.length === 0 && (
                <Empty className="py-12">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <MessageCircleIcon />
                    </EmptyMedia>
                    <EmptyTitle className="text-sm">
                      {query ? "Nothing by that name" : filter === "unread" ? "You're all caught up" : "No conversations yet"}
                    </EmptyTitle>
                    <EmptyDescription>{query ? "Try another spelling." : "Search for anyone to start a chat, or start a group."}</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
            </>
          )}
        </div>
      </aside>

      {withGroup ? (
        <GroupConversation key={withGroup} groupId={withGroup} unread={unreadOf} />
      ) : withUser ? (
        <Conversation key={withUser} userId={withUser} unread={unreadOf} />
      ) : (
        <section className="chat-wallpaper flex flex-1 items-center justify-center max-lg:hidden">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessageCircleIcon />
              </EmptyMedia>
              <EmptyTitle>Your messages</EmptyTitle>
              <EmptyDescription>Pick a conversation, search for anyone, or start a group. Send files and call right from the chat.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </section>
      )}
    </div>
  );
}
