export function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]!.toUpperCase())
    .join("");
}

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 31536000],
  ["month", 2592000],
  ["week", 604800],
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
];

export function relativeTime(iso: string | null | undefined) {
  if (!iso) return "Never";
  const secs = (new Date(iso).getTime() - Date.now()) / 1000;
  for (const [unit, size] of UNITS) {
    if (Math.abs(secs) >= size) return rtf.format(Math.round(secs / size), unit);
  }
  return "Just now";
}

const dateFmt = new Intl.DateTimeFormat("en", { day: "numeric", month: "short", year: "numeric" });
const dateTimeFmt = new Intl.DateTimeFormat("en", { day: "numeric", month: "short", hour: "numeric", minute: "2-digit" });

/** "2026-11-05" → "5 Nov 2026" (dates are calendar days; no timezone shift). */
/** A plain date (YYYY-MM-DD) or a stored moment (ISO 8601); moments keep their time. */
export function formatDate(d: string | null | undefined) {
  if (!d) return "—";
  if (d.includes("T")) return formatDateTime(d);
  const [y, m, day] = d.split("-").map(Number);
  return dateFmt.format(new Date(y!, m! - 1, day!));
}

/** An ISO moment as the value a <input type="datetime-local"> expects, in local time. */
export function toLocalInput(iso: string | null | undefined) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function formatDateTime(iso: string) {
  return dateTimeFmt.format(new Date(iso));
}

export function formatMoney(amount: string | null | undefined, currency: string) {
  if (amount == null) return "—";
  try {
    return new Intl.NumberFormat("en", { style: "currency", currency }).format(Number(amount));
  } catch {
    return `${amount} ${currency}`;
  }
}

export function projectRef(n: number) {
  return `#${n}`;
}

export function formatBytes(n: number) {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}
