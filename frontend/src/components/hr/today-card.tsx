"use client";

import { useEffect, useState } from "react";
import { ClockIcon, LogInIcon, LogOutIcon, MapPinIcon, TimerResetIcon } from "lucide-react";
import { api } from "@/lib/api";
import { clock, currentPosition, hours, scheduleLabel, useHrAction, useOfficeLocation, type HrMe } from "@/lib/hr";
import { formatDate } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

function useNow(intervalMs = 30_000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function TodayCard({ me, onCorrection }: { me: HrMe; onCorrection: () => void }) {
  const now = useNow();
  const rec = me.today_record;
  const tz = me.schedule.timezone;
  const atOffice = useOfficeLocation().data?.enabled ?? false;
  // When HR has set an office location, the server needs to know where this device is.
  const position = async () => (atOffice ? await currentPosition() : undefined);
  const checkIn = useHrAction(async () => api("/hr/me/check-in", { method: "POST", body: await position() }), "Checked in. Have a good day!");
  const checkOut = useHrAction(async () => api("/hr/me/check-out", { method: "POST", body: await position() }), "Checked out. See you tomorrow!");
  const worked = rec ? Math.floor(((rec.check_out_at ? new Date(rec.check_out_at).getTime() : now) - new Date(rec.check_in_at).getTime()) / 60000) : 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ClockIcon className="size-4 text-primary" /> Today
        </CardTitle>
        <CardDescription>
          {formatDate(me.work_date)} · {scheduleLabel(me.schedule)}
          {!me.is_working_day && " · Not a working day"}
        </CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm" onClick={onCorrection}>
            <TimerResetIcon /> Fix a day
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-4">
        <dl className="grid grid-cols-3 gap-6 text-sm">
          <div>
            <dt className="text-xs text-muted-foreground">Checked in</dt>
            <dd className="mt-0.5 flex items-center gap-1.5 text-lg font-semibold tabular-nums">
              {clock(rec?.check_in_at, tz)}
              {me.is_late && <Badge variant="outline" className="border-amber-300 text-amber-700">Late</Badge>}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">Checked out</dt>
            <dd className="mt-0.5 text-lg font-semibold tabular-nums">{clock(rec?.check_out_at, tz)}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{rec && !rec.check_out_at ? "Working for" : "Worked"}</dt>
            <dd className="mt-0.5 text-lg font-semibold tabular-nums">{hours(worked)}</dd>
          </div>
        </dl>
        {!rec || !rec.check_out_at ? (
          <div className="flex flex-col items-end gap-1.5">
            {!rec ? (
              <Button size="lg" onClick={() => checkIn.mutate(undefined)} disabled={checkIn.isPending}>
                <LogInIcon /> {checkIn.isPending && atOffice ? "Finding you…" : "Check in"}
              </Button>
            ) : (
              <Button size="lg" variant="outline" onClick={() => checkOut.mutate(undefined)} disabled={checkOut.isPending}>
                <LogOutIcon /> {checkOut.isPending && atOffice ? "Finding you…" : "Check out"}
              </Button>
            )}
            {atOffice && (
              <p className="flex items-center gap-1 text-xs text-muted-foreground">
                <MapPinIcon className="size-3" /> Works only at the office
              </p>
            )}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">Done for today.</p>
        )}
      </CardContent>
    </Card>
  );
}
