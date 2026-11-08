"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { PlusIcon } from "lucide-react";
import { toast } from "sonner";
import { useCreateGroup } from "@/lib/chat";
import { useDirectory } from "@/lib/hr";
import { useMe } from "@/lib/session";
import { initials } from "@/lib/format";
import { ROLE_LABELS, type Role } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

/** Starts a named group — one for Sales, one for Ops — with whoever should be in it. */
export function NewGroupDialog() {
  const router = useRouter();
  const { data: me } = useMe();
  const directory = useDirectory();
  const create = useCreateGroup();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [find, setFind] = useState("");
  const [picked, setPicked] = useState<string[]>([]);

  const query = find.trim().toLowerCase();
  const people = (directory.data ?? []).filter(
    (p) => p.user_id !== me?.id && p.is_active && (!query || p.name.toLowerCase().includes(query)),
  );

  function reset() {
    setName("");
    setFind("");
    setPicked([]);
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" className="shrink-0">
          <PlusIcon />
          <span className="max-sm:hidden">New group</span>
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              { name: name.trim(), member_ids: picked },
              {
                onSuccess: (r) => {
                  setOpen(false);
                  reset();
                  router.push(`/messages?group=${r.group.id}`);
                },
                onError: (err) => toast.error(err.message),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>New group</DialogTitle>
            <DialogDescription>Everyone you add can read it, write in it and bring others in.</DialogDescription>
          </DialogHeader>

          <div className="my-4 space-y-4">
            <Field>
              <FieldLabel htmlFor="group-name">Name</FieldLabel>
              <Input
                id="group-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Sales"
                maxLength={80}
                required
                autoFocus
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="group-people">People</FieldLabel>
              <Input id="group-people" value={find} onChange={(e) => setFind(e.target.value)} placeholder="Search by name…" />
              <ul className="max-h-56 space-y-1 overflow-y-auto">
                {people.map((p) => {
                  const on = picked.includes(p.user_id);
                  return (
                    <li key={p.user_id}>
                      <button
                        type="button"
                        aria-pressed={on}
                        className={cn(
                          "flex w-full items-center gap-2 rounded-md px-1.5 py-1.5 text-left text-sm hover:bg-muted",
                          on && "bg-accent text-accent-foreground",
                        )}
                        onClick={() => setPicked((cur) => (on ? cur.filter((id) => id !== p.user_id) : [...cur, p.user_id]))}
                      >
                        <Avatar className="size-7">
                          <AvatarFallback className="text-xs">{initials(p.name)}</AvatarFallback>
                        </Avatar>
                        <span className="min-w-0 flex-1 truncate">{p.name}</span>
                        <span className="shrink-0 text-xs text-muted-foreground">{ROLE_LABELS[p.role as Role] ?? p.role}</span>
                      </button>
                    </li>
                  );
                })}
                {people.length === 0 && <li className="px-1.5 py-1.5 text-sm text-muted-foreground">Nobody by that name.</li>}
              </ul>
            </Field>
          </div>

          <DialogFooter>
            <span className="mr-auto self-center text-xs text-muted-foreground">
              {picked.length === 0 ? "Just you so far" : `You and ${picked.length} ${picked.length === 1 ? "other" : "others"}`}
            </span>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending && <Spinner />}
              Create group
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
