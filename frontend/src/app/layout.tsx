import type { Metadata, Viewport } from "next";
// Self-hosted Geist (no build-time fetch from Google Fonts).
import { GeistMono } from "geist/font/mono";
import { GeistSans } from "geist/font/sans";
import "./globals.css";
import { Providers } from "./providers";

export const metadata: Metadata = {
  title: { default: "Growvence Tech Operations", template: "%s · Growvence Tech" },
  description: "Projects, assignments and client updates",
  manifest: "/manifest.json",
  icons: {
    icon: "/icon.png",
    apple: "/apple-icon.png",
  },
  appleWebApp: {
    capable: true,
    title: "Growvence Ops",
    statusBarStyle: "default",
  },
};

export const viewport: Viewport = {
  themeColor: "#46C34C",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // suppressHydrationWarning: next-themes sets the theme class before hydration.
    <html
      lang="en"
      className={`${GeistSans.variable} ${GeistMono.variable} h-full antialiased`}
      suppressHydrationWarning
    >
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
