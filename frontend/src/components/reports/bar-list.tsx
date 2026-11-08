import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

/**
 * Single-series horizontal bars (magnitude by category). One hue, so no legend:
 * the card title names it. Values sit at the bar tip (the brand hue is below 3:1
 * on the surface, so the number never relies on the mark).
 */
export function BarList({ rows, emptyText = "No data" }: { rows: { label: React.ReactNode; value: number; hint?: string }[]; emptyText?: string }) {
  const max = Math.max(1, ...rows.map((r) => r.value));
  if (rows.length === 0) return <p className="text-sm text-muted-foreground">{emptyText}</p>;
  return (
    <ul className="space-y-2.5" role="list">
      {rows.map((r, i) => (
        <li key={i} className="grid grid-cols-[minmax(7rem,9rem)_1fr] items-center gap-3 text-sm">
          <span className="truncate text-muted-foreground">{r.label}</span>
          <Tooltip>
            <TooltipTrigger asChild>
              {/* Hit target is the full row height, larger than the 14px bar. */}
              <div className="flex h-6 cursor-default items-center gap-2" tabIndex={0} aria-label={`${r.hint ?? ""} ${r.value}`}>
                <div
                  className="h-3.5 rounded-r-[4px] bg-primary transition-[width]"
                  style={{ width: `${(r.value / max) * 85}%`, minWidth: r.value ? 4 : 0 }}
                />
                <span className="text-sm font-medium tabular-nums text-foreground">{r.value}</span>
              </div>
            </TooltipTrigger>
            <TooltipContent>
              {r.hint ?? r.label}: <span className="font-semibold tabular-nums">{r.value}</span>
            </TooltipContent>
          </Tooltip>
        </li>
      ))}
    </ul>
  );
}
