import Image from "next/image";
import { cn } from "@/lib/utils";

// Full Growvence lockup (white) for dark surfaces: sidebar, login panel.
export function LogoMark({ className }: { className?: string }) {
  return <Image src="/logo-mark.png" alt="Growvence Tech" width={2243} height={394} priority className={cn("h-7 w-auto", className)} />;
}

// Full Growvence lockup (black/green) for light surfaces.
export function LogoMarkDark({ className }: { className?: string }) {
  return <Image src="/logo-mark-dark.png" alt="Growvence Tech" width={2243} height={394} priority className={cn("h-7 w-auto", className)} />;
}
