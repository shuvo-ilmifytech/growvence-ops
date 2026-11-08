"use client";

import { useState } from "react";
import {
  RepeatIcon,
  PlusIcon,
  PencilIcon,
  TrashIcon,
  PlayIcon,
  PauseIcon,
  CalendarClockIcon,
} from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useRecurringTasks,
  useCreateRecurring,
  useUpdateRecurring,
  useDeleteRecurring,
  useToggleRecurring,
  useRunRecurringNow,
  FREQUENCY_LABELS,
  DAY_NAMES,
  type RecurringTask,
  type RecurringInput,
} from "@/lib/recurring";
import { useProjects } from "@/lib/projects";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";

const NONE = "__none__";

export default function RecurringPage() {
  const { data: me } = useMe();
  const list = useRecurringTasks();
  const [dialog, setDialog] = useState<null | { task?: RecurringTask }>(null);
  const canEdit = me && ["tl", "hr", "ceo"].includes(me.role) || me?.is_admin;

  const tasks = list.data?.recurring_tasks ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Recurring tasks"
        description="Scheduled tasks that automatically create board cards on a daily, weekly or monthly cadence."
        actions={
          canEdit && (
            <Button onClick={() => setDialog({})}>
              <PlusIcon /> New recurring task
            </Button>
          )
        }
      />

      {list.isPending ? (
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-44" />
          ))}
        </div>
      ) : tasks.length === 0 ? (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <RepeatIcon />
            </EmptyMedia>
            <EmptyTitle>No recurring tasks yet</EmptyTitle>
            <EmptyDescription>
              {canEdit
                ? "Create one to auto-generate board cards on a schedule — e.g. a weekly QA checklist."
                : "No recurring tasks have been set up yet."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
          {tasks.map((t) => (
            <TaskCard key={t.id} task={t} canEdit={!!canEdit} onEdit={() => setDialog({ task: t })} />
          ))}
        </div>
      )}

      {dialog && (
        <TaskDialog
          task={dialog.task}
          onClose={() => setDialog(null)}
        />
      )}
    </div>
  );
}

function scheduleLabel(t: RecurringTask): string {
  if (t.frequency === "daily") return "Every day";
  if (t.frequency === "weekly")
    return `Every ${t.day_of_week != null ? DAY_NAMES[t.day_of_week] : "week"}`;
  return `Monthly on day ${t.day_of_month ?? 1}`;
}

function TaskCard({ task, canEdit, onEdit }: { task: RecurringTask; canEdit: boolean; onEdit: () => void }) {
  const toggle = useToggleRecurring();
  const runNow = useRunRecurringNow();
  const del = useDeleteRecurring();
  const [confirm, setConfirm] = useState(false);

  return (
    <Card className={cn(!task.is_active && "opacity-60")}>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="text-base leading-snug">{task.title}</CardTitle>
          <Badge variant={task.frequency === "daily" ? "default" : "secondary"}>
            {FREQUENCY_LABELS[task.frequency]}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {task.description && (
          <p className="line-clamp-2 text-sm text-muted-foreground">{task.description}</p>
        )}
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="flex items-center gap-1">
            <RepeatIcon className="size-3.5" /> {scheduleLabel(task)}
          </span>
          <span className="flex items-center gap-1">
            <CalendarClockIcon className="size-3.5" /> Next: {task.next_run}
          </span>
        </div>
        {task.project_name && (
          <p className="text-xs text-muted-foreground">Project: {task.project_name}</p>
        )}
        {task.last_run && (
          <p className="text-xs text-muted-foreground">Last ran: {task.last_run}</p>
        )}
        {canEdit && (
          <div className="flex items-center justify-between border-t pt-3">
            <div className="flex items-center gap-2">
              <Switch
                checked={task.is_active}
                onCheckedChange={() => toggle.mutate(task.id)}
                aria-label={task.is_active ? "Pause" : "Activate"}
              />
              <span className="text-xs text-muted-foreground">
                {task.is_active ? "Active" : "Paused"}
              </span>
            </div>
            <div className="flex gap-1">
              <Button
                variant="ghost"
                size="icon"
                title="Run now"
                onClick={() => runNow.mutate(task.id)}
                disabled={runNow.isPending}
              >
                <PlayIcon className="size-4" />
              </Button>
              <Button variant="ghost" size="icon" title="Edit" onClick={onEdit}>
                <PencilIcon className="size-4" />
              </Button>
              {confirm ? (
                <Button
                  variant="ghost"
                  size="icon"
                  title="Confirm delete"
                  className="text-destructive"
                  onClick={() => del.mutate(task.id)}
                  disabled={del.isPending}
                >
                  <TrashIcon className="size-4" />
                </Button>
              ) : (
                <Button
                  variant="ghost"
                  size="icon"
                  title="Delete"
                  onClick={() => setConfirm(true)}
                >
                  <TrashIcon className="size-4" />
                </Button>
              )}
            </div>
          </div>
        )}
        {confirm && (
          <p className="text-xs text-destructive">
            Click the trash icon again to confirm deletion.{" "}
            <button className="underline" onClick={() => setConfirm(false)}>Cancel</button>
          </p>
        )}
        {!task.is_active && !canEdit && (
          <Badge variant="outline">Paused</Badge>
        )}
      </CardContent>
    </Card>
  );
}

function TaskDialog({ task, onClose }: { task?: RecurringTask; onClose: () => void }) {
  const create = useCreateRecurring();
  const update = useUpdateRecurring();
  const projects = useProjects({ page_size: 100 });

  const [title, setTitle] = useState(task?.title ?? "");
  const [description, setDescription] = useState(task?.description ?? "");
  const [projectId, setProjectId] = useState<string>(task?.project_id ?? NONE);
  const [frequency, setFrequency] = useState<"daily" | "weekly" | "monthly">(task?.frequency ?? "weekly");
  const [dayOfWeek, setDayOfWeek] = useState<string>(String(task?.day_of_week ?? 1));
  const [dayOfMonth, setDayOfMonth] = useState<string>(String(task?.day_of_month ?? 1));
  const [nextRun, setNextRun] = useState(task?.next_run ?? new Date().toISOString().slice(0, 10));

  const pending = create.isPending || update.isPending;

  function onSubmit() {
    const input: RecurringInput = {
      title: title.trim(),
      description: description.trim(),
      project_id: projectId === NONE ? null : projectId,
      frequency,
      day_of_week: frequency === "weekly" ? parseInt(dayOfWeek, 10) : null,
      day_of_month: frequency === "monthly" ? parseInt(dayOfMonth, 10) : null,
      next_run: nextRun,
    };
    if (task) {
      update.mutate({ ...input, id: task.id }, { onSuccess: onClose });
    } else {
      create.mutate(input, { onSuccess: onClose });
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{task ? "Edit recurring task" : "New recurring task"}</DialogTitle>
          <DialogDescription>
            A board card will be created automatically on each run.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Field>
            <FieldLabel>Title</FieldLabel>
            <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="e.g. Weekly QA checklist" />
          </Field>
          <Field>
            <FieldLabel>Description</FieldLabel>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What should the card say?" rows={3} />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field>
              <FieldLabel>Project</FieldLabel>
              <Select value={projectId} onValueChange={setProjectId}>
                <SelectTrigger>
                  <SelectValue placeholder="Select project" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>No project (standalone)</SelectItem>
                  {(projects.data?.projects ?? []).map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      #{p.number} {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel>Frequency</FieldLabel>
              <Select value={frequency} onValueChange={(v) => setFrequency(v as typeof frequency)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="daily">Daily</SelectItem>
                  <SelectItem value="weekly">Weekly</SelectItem>
                  <SelectItem value="monthly">Monthly</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-4">
            {frequency === "weekly" && (
              <Field>
                <FieldLabel>Day of week</FieldLabel>
                <Select value={dayOfWeek} onValueChange={setDayOfWeek}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DAY_NAMES.map((d, i) => (
                      <SelectItem key={i} value={String(i)}>{d}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
            {frequency === "monthly" && (
              <Field>
                <FieldLabel>Day of month</FieldLabel>
                <Input
                  type="number"
                  min={1}
                  max={31}
                  value={dayOfMonth}
                  onChange={(e) => setDayOfMonth(e.target.value)}
                />
              </Field>
            )}
            <Field>
              <FieldLabel>First run</FieldLabel>
              <Input type="date" value={nextRun} onChange={(e) => setNextRun(e.target.value)} />
            </Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={onSubmit} disabled={pending || !title.trim()}>
            {pending ? "Saving…" : task ? "Save changes" : "Create task"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
