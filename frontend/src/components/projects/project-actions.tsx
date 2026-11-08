"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ArrowRightLeftIcon, CalendarClockIcon, MoreHorizontalIcon, PencilIcon, UserPlusIcon, XCircleIcon } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api";
import { formatDate } from "@/lib/format";
import { useDepartments } from "@/lib/hr";
import { useAssign, useMembers, useMoveTeam, usePlan, useTransition } from "@/lib/projects";
import { useMe } from "@/lib/session";
import { useDirectoryByRole, useTeams } from "@/lib/teams";
import { PRIORITIES, PRIORITY_LABELS, type Priority, type ProjectDetail, type WorkflowAction } from "@/lib/types";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";

/** Copy for the comment dialog, keyed by "from>to". */
function dialogCopy(a: WorkflowAction, revisions: number): { title: string; prompt: string; placeholder: string } {
  if (a.to === "cancelled")
    return { title: "Cancel this project?", prompt: "It will be closed for everyone. This can't be undone.", placeholder: "Reason (optional)" };
  if (a.revision)
    return {
      title: "Client requested a revision",
      prompt: `The project goes back to In Progress for the team. This will be revision ${revisions + 1}.`,
      placeholder: "What did the client ask to change?",
    };
  switch (`${a.from}>${a.to}`) {
    case "in_progress>blocked":
      return { title: "Mark as blocked", prompt: "The Team Leader will see what's blocking you.", placeholder: "What's blocking this project?" };
    case "ready_for_review>in_progress":
    case "ready_to_deliver>in_progress":
      return { title: "Send back to the team", prompt: "The project goes back to In Progress.", placeholder: "What needs fixing?" };
  }
  return { title: a.label, prompt: "", placeholder: "Add a note" };
}

export function ProjectActions({ detail }: { detail: ProjectDetail }) {
  const { project: p, permissions, actions } = detail;
  const transition = useTransition(p.id);
  const [dialog, setDialog] = useState<WorkflowAction | null>(null);
  const [assignOpen, setAssignOpen] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);

  const cancel = actions.find((a) => a.to === "cancelled");
  const main = actions.filter((a) => a.to !== "cancelled");

  function run(a: WorkflowAction) {
    if (a.needs_comment || a.to === "cancelled") {
      setDialog(a);
      return;
    }
    transition.mutate(
      { to: a.to },
      {
        onSuccess: () => toast.success(`${a.label}: done`),
        onError: (err) => toast.error(fieldOrMessage(err)),
      },
    );
  }

  const hasMenu = !!cancel || permissions.can_plan || permissions.can_move_team;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {permissions.can_edit && (
        <Button variant="outline" asChild>
          <Link href={`/projects/${p.id}/edit`}>
            <PencilIcon /> Edit
          </Link>
        </Button>
      )}
      {permissions.can_assign && (
        <Button variant={p.status === "new" ? "default" : "outline"} onClick={() => setAssignOpen(true)}>
          <UserPlusIcon /> {p.member_ids.length ? "Members" : "Assign"}
        </Button>
      )}
      {main.map((a, i) => (
        <Button
          key={a.to}
          variant={i === 0 && !(permissions.can_assign && p.status === "new") ? "default" : "outline"}
          disabled={transition.isPending}
          onClick={() => run(a)}
        >
          {transition.isPending && transition.variables?.to === a.to && <Spinner />}
          {a.label}
        </Button>
      ))}
      {hasMenu && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="More actions">
              <MoreHorizontalIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            {permissions.can_plan && (
              <DropdownMenuItem onSelect={() => setPlanOpen(true)}>
                <CalendarClockIcon /> Change priority & due date
              </DropdownMenuItem>
            )}
            {permissions.can_move_team && (
              <DropdownMenuItem onSelect={() => setMoveOpen(true)}>
                <ArrowRightLeftIcon /> Move to another team
              </DropdownMenuItem>
            )}
            {(permissions.can_plan || permissions.can_move_team) && cancel && <DropdownMenuSeparator />}
            {cancel && (
              <DropdownMenuItem variant="destructive" onSelect={() => setDialog(cancel)}>
                <XCircleIcon /> Cancel project
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}

      <CommentTransitionDialog
        action={dialog}
        revisions={p.revision_count}
        onClose={() => setDialog(null)}
        onConfirm={(a, comment) =>
          transition.mutateAsync({ to: a.to, comment }).then(() => {
            toast.success(a.to === "cancelled" ? "Project cancelled" : `${a.label}: done`);
            setDialog(null);
          })
        }
      />
      {permissions.can_assign && <AssignDialog open={assignOpen} onOpenChange={setAssignOpen} detail={detail} />}
      {permissions.can_plan && <PlanDialog open={planOpen} onOpenChange={setPlanOpen} detail={detail} />}
      {permissions.can_move_team && <MoveTeamDialog open={moveOpen} onOpenChange={setMoveOpen} detail={detail} />}
    </div>
  );
}

function fieldOrMessage(err: unknown) {
  if (err instanceof ApiError) {
    const first = Object.entries(err.fields)[0];
    return first ? `${first[0].replaceAll("_", " ")} ${first[1]}` : err.message;
  }
  return err instanceof Error ? err.message : "Something went wrong";
}

function CommentTransitionDialog({
  action,
  revisions,
  onClose,
  onConfirm,
}: {
  action: WorkflowAction | null;
  revisions: number;
  onClose: () => void;
  onConfirm: (a: WorkflowAction, comment: string) => Promise<void>;
}) {
  const [comment, setComment] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const copy = action ? dialogCopy(action, revisions) : null;

  return (
    <Dialog
      open={!!action}
      onOpenChange={(o) => {
        if (!o) {
          onClose();
          setComment("");
          setError("");
        }
      }}
    >
      <DialogContent className="sm:max-w-lg">
        {action && copy && (
          <form
            className="grid gap-5"
            onSubmit={async (e) => {
              e.preventDefault();
              if (action.needs_comment && !comment.trim()) {
                setError("Please add a short note. It's required for this step.");
                return;
              }
              setPending(true);
              setError("");
              try {
                await onConfirm(action, comment.trim());
                setComment("");
              } catch (err) {
                setError(fieldOrMessage(err));
              } finally {
                setPending(false);
              }
            }}
          >
            <DialogHeader>
              <DialogTitle>{copy.title}</DialogTitle>
              {copy.prompt && <DialogDescription>{copy.prompt}</DialogDescription>}
            </DialogHeader>
            <Field data-invalid={!!error || undefined}>
              <Textarea autoFocus rows={4} value={comment} onChange={(e) => setComment(e.target.value)} placeholder={copy.placeholder} />
              <FieldError>{error}</FieldError>
            </Field>
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline">
                  {action.to === "cancelled" ? "Keep project" : "Cancel"}
                </Button>
              </DialogClose>
              <Button
                type="submit"
                disabled={pending}
                className={action.to === "cancelled" ? "bg-destructive text-white hover:bg-destructive/90" : undefined}
              >
                {pending && <Spinner />}
                {action.to === "cancelled" ? "Cancel project" : action.label}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function AssignDialog({ open, onOpenChange, detail }: { open: boolean; onOpenChange: (o: boolean) => void; detail: ProjectDetail }) {
  const p = detail.project;
  const members = useMembers();
  const leads = useDirectoryByRole("tl");
  const teams = useTeams();
  const departments = useDepartments();
  const me = useMe().data;
  // A project routed to a team can only go to that team's members, or to a leader who
  // runs the team: the team's lead, its department's head, or the leader assigning it.
  const team = teams.data?.find((t) => t.id === p.team_id);
  const head = departments.data?.find((d) => d.id === team?.department_id)?.head_id;
  const leaderIds = new Set([me?.id, team?.lead_id, head].filter(Boolean));
  const leaders = (leads.data ?? []).filter((u) => leaderIds.has(u.id));
  const choices = members.data && [
    ...leaders.map((u) => ({ ...u, name: u.id === me?.id ? `${u.name} (me)` : `${u.name} (lead)` })),
    ...members.data.filter((m) => !p.team_id || m.team_id === p.team_id),
  ];
  const assign = useAssign(p.id);
  const [picked, setPicked] = useState<string[]>(p.member_ids);
  const [priority, setPriority] = useState<Priority>(p.priority);
  // The TL's internal due date stays a plain day; it defaults to the delivery deadline's day.
  const [due, setDue] = useState(p.due_date ?? p.deadline?.slice(0, 10) ?? "");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const reassign = p.member_ids.length > 0;
  const toggle = (id: string) => setPicked((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            setErrors({});
            assign.mutate(
              { assignee_ids: picked, priority, due_date: due || null },
              {
                onSuccess: (d) => {
                  toast.success(`${reassign ? "Members updated" : "Assigned"}: ${d.project.member_names.join(", ")}`);
                  onOpenChange(false);
                },
                onError: (err) => {
                  if (err instanceof ApiError && Object.keys(err.fields).length) setErrors(err.fields);
                  else setErrors({ _: fieldOrMessage(err) });
                },
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>{reassign ? "Change members" : "Assign project"}</DialogTitle>
            <DialogDescription>
              {reassign
                ? "Add or remove members; the work carries on where it is. Replacing everyone starts it again from Assigned."
                : "Pick who works on it (you can take it yourself), and when it's due."}
              {p.team_name && ` Only the ${p.team_name} team and its leaders can take it.`}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field data-invalid={!!errors.assignee_ids || undefined}>
              <FieldLabel>Members</FieldLabel>
              {members.isPending ? (
                <span className="text-sm text-muted-foreground">Loading…</span>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {choices?.map((m) => {
                    const on = picked.includes(m.id);
                    return (
                      <button
                        key={m.id}
                        type="button"
                        aria-pressed={on}
                        onClick={() => toggle(m.id)}
                        className={`rounded-full border px-3 py-1 text-sm transition-colors ${on ? "border-primary bg-primary/10" : "text-muted-foreground hover:bg-muted"}`}
                      >
                        {m.name}
                      </button>
                    );
                  })}
                  {choices?.length === 0 && <span className="text-sm text-muted-foreground">No members in this team yet.</span>}
                </div>
              )}
              <FieldError>{errors.assignee_ids}</FieldError>
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel>Priority</FieldLabel>
                <Select value={priority} onValueChange={(v) => setPriority(v as Priority)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PRIORITIES.map((x) => (
                      <SelectItem key={x} value={x}>
                        {PRIORITY_LABELS[x]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field data-invalid={!!errors.due_date || undefined}>
                <FieldLabel htmlFor="due">Due date</FieldLabel>
                <Input id="due" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
                <FieldError>{errors.due_date && `Due date ${errors.due_date}`}</FieldError>
              </Field>
            </div>
            {p.deadline && <FieldDescription>Delivery deadline: {formatDate(p.deadline)}</FieldDescription>}
            {errors._ && <p className="text-sm text-destructive">{errors._}</p>}
          </FieldGroup>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!picked.length || assign.isPending}>
              {assign.isPending && <Spinner />}
              {reassign ? "Save members" : "Assign"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function PlanDialog({ open, onOpenChange, detail }: { open: boolean; onOpenChange: (o: boolean) => void; detail: ProjectDetail }) {
  const p = detail.project;
  const plan = usePlan(p.id);
  const [priority, setPriority] = useState<Priority>(p.priority);
  const [due, setDue] = useState(p.due_date ?? "");
  const [error, setError] = useState("");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            setError("");
            plan.mutate(
              { priority, due_date: due || null },
              {
                onSuccess: () => {
                  toast.success("Priority and due date updated");
                  onOpenChange(false);
                },
                onError: (err) => setError(fieldOrMessage(err)),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>Priority & due date</DialogTitle>
            <DialogDescription>Changes apply without reassigning.</DialogDescription>
          </DialogHeader>
          <div className="grid grid-cols-2 gap-4">
            <Field>
              <FieldLabel>Priority</FieldLabel>
              <Select value={priority} onValueChange={(v) => setPriority(v as Priority)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRIORITIES.map((x) => (
                    <SelectItem key={x} value={x}>
                      {PRIORITY_LABELS[x]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="plan-due">Due date</FieldLabel>
              <Input id="plan-due" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
            </Field>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={plan.isPending}>
              {plan.isPending && <Spinner />}
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MoveTeamDialog({ open, onOpenChange, detail }: { open: boolean; onOpenChange: (o: boolean) => void; detail: ProjectDetail }) {
  const p = detail.project;
  const router = useRouter();
  const teams = useTeams();
  const move = useMoveTeam(p.id);
  const [teamId, setTeamId] = useState("");
  const [comment, setComment] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const others = (teams.data ?? []).filter((t) => t.id !== p.team_id);
  const target = others.find((t) => t.id === teamId);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            setErrors({});
            move.mutate(
              { team_id: teamId, comment },
              {
                onSuccess: (res) => {
                  onOpenChange(false);
                  if ("project" in res) {
                    toast.success(`Moved to ${res.project.team_name}`);
                    return;
                  }
                  toast.success(`Moved to ${target?.name ?? "the new team"}. Its lead takes it from here.`);
                  router.push("/projects");
                },
                onError: (err) => {
                  if (err instanceof ApiError && Object.keys(err.fields).length) setErrors(err.fields);
                  else setErrors({ _: fieldOrMessage(err) });
                },
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>Move to another team</DialogTitle>
            <DialogDescription>
              {p.team_name ? `It leaves ${p.team_name}. ` : ""}
              Members who aren&apos;t in the new team come off it. If nobody is left, it goes back to New for the new team&apos;s lead
              to assign. Files, the board and the timeline stay with the project.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field data-invalid={!!errors.team_id || undefined}>
              <FieldLabel>Team</FieldLabel>
              <Select value={teamId} onValueChange={setTeamId}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={teams.isPending ? "Loading…" : "Pick a team"} />
                </SelectTrigger>
                <SelectContent>
                  {others.map((t) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldError>{errors.team_id && `Team ${errors.team_id}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.comment || undefined}>
              <FieldLabel htmlFor="move-reason">Reason</FieldLabel>
              <Textarea
                id="move-reason"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder="e.g. Design is done, the build is Web Dev's"
                rows={3}
              />
              <FieldError>{errors.comment && `Reason: ${errors.comment}`}</FieldError>
            </Field>
            {errors._ && <p className="text-sm text-destructive">{errors._}</p>}
          </FieldGroup>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!teamId || !comment.trim() || move.isPending}>
              {move.isPending && <Spinner />}
              Move project
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
