"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { CheckIcon, LogOutIcon, PencilIcon, UserPlusIcon, UsersRoundIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import {
  useAddGroupMembers,
  useGroupThread,
  useMarkGroupRead,
  useRemoveGroupMember,
  useRenameGroup,
  useSendGroupMessage,
  type GroupMember,
} from "@/lib/chat";
import { useDirectory } from "@/lib/hr";
import { useMe } from "@/lib/session";
import { ROLE_LABELS, type Role } from "@/lib/types";
import { ChatHeader } from "@/components/messages/conversation";
import { Composer, MessageList } from "@/components/messages/message-list";
import { PersonAvatar } from "@/components/messages/person-avatar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";

/** A named conversation with a roster: everyone in it reads, writes and shares files. */
export function GroupConversation({ groupId, unread }: { groupId: string; unread: number }) {
  const router = useRouter();
  const thread = useGroupThread(groupId);
  const send = useSendGroupMessage(groupId);
  const [renaming, setRenaming] = useState(false);
  const pane = useRef<HTMLElement>(null);
  useMarkGroupRead(groupId, unread);

  const group = thread.data?.group;
  const members = thread.data?.members ?? [];

  return (
    <section ref={pane} className="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <ChatHeader onBack={() => router.push("/messages")}>
        {thread.isPending ? (
          <Skeleton className="h-10 w-48" />
        ) : (
          <>
            <PersonAvatar name={group?.name ?? ""} group />
            <div className="min-w-0 flex-1">
              {renaming && group ? (
                <RenameGroup groupId={groupId} name={group.name} onDone={() => setRenaming(false)} />
              ) : (
                <div className="flex items-center gap-1.5">
                  <span className="truncate font-semibold">{group?.name}</span>
                  {group?.mine && (
                    <Button variant="ghost" size="icon-xs" aria-label="Rename group" onClick={() => setRenaming(true)}>
                      <PencilIcon />
                    </Button>
                  )}
                </div>
              )}
              <div className="truncate text-xs text-muted-foreground">
                {members.length} {members.length === 1 ? "member" : "members"}
                {members.length > 0 && ` · ${members.map((m) => m.name.split(" ")[0]).join(", ")}`}
              </div>
            </div>
            <div className="flex -space-x-1.5 max-sm:hidden">
              {members.slice(0, 4).map((m) => (
                <PersonAvatar key={m.id} name={m.name} small className="size-7 ring-2 ring-background" />
              ))}
            </div>
            <MembersDialog groupId={groupId} members={members} owner={group?.created_by ?? ""} />
          </>
        )}
      </ChatHeader>

      <MessageList
        messages={thread.data?.messages ?? []}
        pending={thread.isPending}
        showSenders
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersRoundIcon />
              </EmptyMedia>
              <EmptyTitle className="text-sm">Nothing here yet</EmptyTitle>
              <EmptyDescription>Say the first thing in {group?.name ?? "this group"}, or share a file.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />

      <Composer
        pending={send.isPending}
        dropTarget={pane}
        onSend={(m, restore) =>
          send.mutate(m, {
            onError: (err) => {
              restore();
              toast.error(err.message);
            },
          })
        }
      />
    </section>
  );
}

function RenameGroup({ groupId, name, onDone }: { groupId: string; name: string; onDone: () => void }) {
  const rename = useRenameGroup(groupId);
  const [value, setValue] = useState(name);

  return (
    <form
      className="flex items-center gap-1"
      onSubmit={(e) => {
        e.preventDefault();
        const next = value.trim();
        if (!next || next === name) return onDone();
        rename.mutate(next, { onSuccess: onDone, onError: (err) => toast.error(err.message) });
      }}
    >
      <Input value={value} onChange={(e) => setValue(e.target.value)} maxLength={80} autoFocus aria-label="Group name" className="h-7" />
      <Button type="submit" size="icon-xs" variant="ghost" aria-label="Save name" disabled={rename.isPending}>
        <CheckIcon />
      </Button>
      <Button type="button" size="icon-xs" variant="ghost" aria-label="Cancel" onClick={onDone}>
        <XIcon />
      </Button>
    </form>
  );
}

/** Who is in the room: add anyone, remove (if you started it), or leave. */
function MembersDialog({ groupId, members, owner }: { groupId: string; members: GroupMember[]; owner: string }) {
  const router = useRouter();
  const { data: me } = useMe();
  const directory = useDirectory();
  const add = useAddGroupMembers(groupId);
  const remove = useRemoveGroupMember(groupId);
  const [open, setOpen] = useState(false);
  const [find, setFind] = useState("");

  const query = find.trim().toLowerCase();
  const inGroup = new Set(members.map((m) => m.id));
  const canAdd = (directory.data ?? [])
    .filter((p) => p.is_active && !inGroup.has(p.user_id) && (!query || p.name.toLowerCase().includes(query)))
    .slice(0, 8);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="icon" aria-label="People in this group" title="People in this group">
          <UserPlusIcon />
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>People in this group</DialogTitle>
          <DialogDescription>Anyone here can add someone. Whoever started the group removes people.</DialogDescription>
        </DialogHeader>

        <ul className="max-h-56 space-y-1 overflow-y-auto">
          {members.map((m) => (
            <li key={m.id} className="flex items-center gap-2 rounded-md px-1 py-1.5 text-sm">
              <PersonAvatar name={m.name} className="size-7" />
              <span className="min-w-0 flex-1 truncate">
                {m.name}
                {m.id === owner && <span className="ml-1.5 text-xs text-muted-foreground">started it</span>}
              </span>
              <span className="shrink-0 text-xs text-muted-foreground">{ROLE_LABELS[m.role as Role] ?? m.role}</span>
              {(me?.id === owner || m.id === me?.id) && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={m.id === me?.id ? "Leave group" : `Remove ${m.name}`}
                  disabled={remove.isPending}
                  onClick={() =>
                    remove.mutate(m.id, {
                      onSuccess: () => {
                        if (m.id === me?.id) {
                          setOpen(false);
                          router.push("/messages");
                        }
                      },
                      onError: (err) => toast.error(err.message),
                    })
                  }
                >
                  {m.id === me?.id ? <LogOutIcon /> : <XIcon />}
                </Button>
              )}
            </li>
          ))}
        </ul>

        <div className="space-y-2 border-t pt-3">
          <Input value={find} onChange={(e) => setFind(e.target.value)} placeholder="Add someone…" aria-label="Add someone" />
          <ul className="max-h-40 space-y-1 overflow-y-auto">
            {canAdd.map((p) => (
              <li key={p.user_id}>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 rounded-md px-1 py-1.5 text-left text-sm hover:bg-muted"
                  disabled={add.isPending}
                  onClick={() => {
                    setFind("");
                    add.mutate([p.user_id], { onError: (err) => toast.error(err.message) });
                  }}
                >
                  <UserPlusIcon className="size-4 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{p.name}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{ROLE_LABELS[p.role as Role] ?? p.role}</span>
                </button>
              </li>
            ))}
            {canAdd.length === 0 && <li className="px-1 py-1.5 text-sm text-muted-foreground">Everyone else is already here.</li>}
          </ul>
        </div>
      </DialogContent>
    </Dialog>
  );
}
