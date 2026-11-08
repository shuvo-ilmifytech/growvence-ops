"use client";

import Link from "next/link";
import { Suspense } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import {
  Building2Icon,
  ChevronsUpDownIcon,
  FileTextIcon,
  FolderKanbanIcon,
  ActivityIcon,
  BarChart3Icon,
  ChartNoAxesCombinedIcon,
  CalendarCheck2Icon,
  CalendarClockIcon,
  ContactIcon,
  HandCoinsIcon,
  HandshakeIcon,
  KanbanIcon,
  LandmarkIcon,
  ReceiptTextIcon,
  ReceiptIcon,
  NetworkIcon,
  InboxIcon,
  IdCardIcon,
  BriefcaseBusinessIcon,
  BookOpenIcon,
  BanknoteIcon,
  WalletIcon,
  LayoutDashboardIcon,
  LayoutTemplateIcon,
  LightbulbIcon,
  ListChecksIcon,
  MegaphoneIcon,
  MessageSquareIcon,
  LogOutIcon,
  RepeatIcon,
  SettingsIcon,
  StarIcon,
  TagsIcon,
  TimerIcon,
  UserRoundIcon,
  UsersIcon,
  UsersRoundIcon,
  type LucideIcon,
} from "lucide-react";
import { api } from "@/lib/api";
import { useMe } from "@/lib/session";
import { initials } from "@/lib/format";
import { projectRoleOf, ROLE_LABELS } from "@/lib/types";
import { LogoMarkDark } from "@/components/brand";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { NotificationBell } from "@/components/notification-bell";
import { ThemeToggle } from "@/components/theme-toggle";
import { useNotificationStream } from "@/lib/notifications";
import { GlobalSearch } from "@/components/global-search";
import { RingBanner } from "@/components/ring-banner";
import { CallOverlay } from "@/components/calls/call-overlay";
import { CallProvider } from "@/lib/calls";
import { useChatStream, useGroupStream, useInbox } from "@/lib/chat";
import { useDirectory, useHrMe } from "@/lib/hr";
import { useInvoices } from "@/lib/finance";

type NavItem = { href: string; label: string; icon: LucideIcon };

/** Client portal: external clients see only their projects. */
const CLIENT_WORKSPACE: NavItem[] = [
  { href: "/client", label: "My Projects", icon: BriefcaseBusinessIcon },
];

/** Investor portal: capital partners see only their own share. */
const INVESTOR_WORKSPACE: NavItem[] = [
  { href: "/investor", label: "My Share", icon: HandCoinsIcon },
];

const WORKSPACE: NavItem[] = [
  { href: "/", label: "Dashboard", icon: LayoutDashboardIcon },
  { href: "/my-work", label: "My work", icon: ListChecksIcon },
  { href: "/projects", label: "Projects", icon: FolderKanbanIcon },
  { href: "/templates", label: "Templates", icon: LayoutTemplateIcon },
  { href: "/recurring", label: "Recurring", icon: RepeatIcon },
  { href: "/updates", label: "Updates", icon: MegaphoneIcon },
  { href: "/reviews", label: "Reviews", icon: StarIcon },
  { href: "/requests", label: "No-project requests", icon: InboxIcon },
  { href: "/board", label: "Board", icon: KanbanIcon },
  { href: "/messages", label: "Messages", icon: MessageSquareIcon },
  { href: "/kb", label: "Knowledge base", icon: BookOpenIcon },
  { href: "/deadlines", label: "Deadlines", icon: CalendarClockIcon },
  { href: "/time", label: "Time tracking", icon: TimerIcon },
];

// People pages are for HR and the CEO; system setup for admins and the CEO.
const PEOPLE_ADMIN: NavItem[] = [
  { href: "/admin/users", label: "Users", icon: UsersIcon },
  { href: "/admin/teams", label: "Teams", icon: UsersRoundIcon },
  { href: "/admin/hr", label: "HR settings", icon: BriefcaseBusinessIcon },
];
const SYSTEM_ADMIN: NavItem[] = [
  { href: "/admin/lookups", label: "Profiles & types", icon: TagsIcon },
  { href: "/admin/settings", label: "Settings", icon: SettingsIcon },
];

// Longest matching prefix wins.
const TITLES: [string, string][] = [
  ["/my-work", "My work"],
  ["/projects/new", "New project"],
  ["/projects", "Projects"],
  ["/templates", "Templates"],
  ["/recurring", "Recurring"],
  ["/updates", "Updates"],
  ["/reviews", "Reviews"],
  ["/requests", "No-project requests"],
  ["/board", "Board"],
  ["/messages", "Messages"],
  ["/kb", "Knowledge base"],
  ["/deadlines", "Deadlines"],
  ["/time", "Time tracking"],
  ["/billing", "Billing"],
  ["/payments", "Payments"],
  ["/clients", "Clients"],
  ["/contracts", "Contracts"],
  ["/retainers", "Retainers"],
  ["/b2b", "B2B"],
  ["/investors", "Investors"],
  ["/investor", "My Share"],
  ["/client", "My Projects"],
  ["/reports", "Reports"],
  ["/analytics", "Analytics"],
  ["/insights", "Insights"],
  ["/activity", "Activity"],
  ["/hr/approvals", "Approvals"],
  ["/hr/team", "Team attendance"],
  ["/hr/people", "People"],
  ["/hr/org", "Org chart"],
  ["/hr/salaries", "Salaries"],
  ["/finance", "Finance"],
  ["/payroll", "Payroll"],
  ["/invoices", "Invoices"],
  ["/hr", "My HR"],
  ["/admin/hr", "HR settings"],
  ["/admin/users", "Users"],
  ["/admin/teams", "Teams"],
  ["/admin/lookups", "Profiles & types"],
  ["/admin/settings", "Settings"],
  ["/account", "Account"],
];

function titleFor(pathname: string) {
  if (pathname === "/") return "Dashboard";
  return TITLES.find(([prefix]) => pathname.startsWith(prefix))?.[1] ?? "";
}

// usePathname() is request data on dynamic routes (/projects/[id]), so every
// reader sits behind <Suspense> to keep the shell prerenderable.
function HeaderTitle() {
  return <span className="text-sm font-medium">{titleFor(usePathname())}</span>;
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const { data: me } = useMe();
  useNotificationStream(!!me);
  useChatStream(!!me);
  useGroupStream(!!me);
  return (
    <CallProvider enabled={!!me}>
    <SidebarProvider>
      <AppSidebar />
      {/* min-w-0: let wide tables scroll inside their card instead of widening the page */}
      <SidebarInset className="min-w-0">
        {/* data-print-hide: the app's own chrome is left off a printed page. */}
        <header
          data-print-hide
          className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur"
        >
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-1 data-vertical:h-4 data-vertical:self-center" />
          <Suspense>
            <HeaderTitle />
          </Suspense>
          <div className="ml-auto flex items-center gap-2">
            {me && <GlobalSearch />}
            <ThemeToggle />
            {me && <NotificationBell />}
          </div>
        </header>
        <main className="mx-auto w-full max-w-6xl flex-1 p-4 md:p-8">{children}</main>
        <RingBanner enabled={!!me} />
        <CallOverlay />
      </SidebarInset>
    </SidebarProvider>
    </CallProvider>
  );
}

function AppSidebar() {
  const { data: me } = useMe();
  const canSeeInsights = !!me && (me.is_admin || me.role === "ceo" || me.role === "tl" || me.role === "hr");
  const oversight: NavItem[] = [
    ...(me?.permissions.includes("oversight.view_analytics") ? [{ href: "/analytics", label: "Analytics", icon: ChartNoAxesCombinedIcon }] : []),
    ...(me?.permissions.includes("oversight.view_dashboard") ? [{ href: "/reports", label: "Reports", icon: BarChart3Icon }] : []),
    ...(canSeeInsights ? [{ href: "/insights", label: "Insights", icon: LightbulbIcon }] : []),
    ...(me?.permissions.includes("oversight.view_activity") ? [{ href: "/activity", label: "Activity", icon: ActivityIcon }] : []),
  ];
  const hr = useHrNav(!!me);
  const managesPeople = me?.role === "ceo" || me?.role === "hr";
  const systemAdmin = !!me && (me.is_admin || me.role === "ceo");
  const admin = [...(managesPeople ? PEOPLE_ADMIN : []), ...(systemAdmin ? SYSTEM_ADMIN : [])];
  // HR has no project access: only the dashboard (their HR home).
  // Sales only see their own client requests on the board, so it's named for that.
  // HR and the Office Admin have no project pages, but chat is for everyone.
  const unreadMessages = useInbox(!!me).data?.unread ?? 0;
  const label = (i: NavItem) =>
    i.href === "/messages" && unreadMessages > 0 ? { ...i, label: `Messages (${unreadMessages})` } : i;
  const workspace = (
    me?.role === "client"
      ? CLIENT_WORKSPACE
      : me?.role === "investor"
        ? INVESTOR_WORKSPACE
        : me && !projectRoleOf(me.role)
          ? WORKSPACE.filter((i) => i.href === "/" || i.href === "/messages")
          : WORKSPACE.map((i) => (i.href === "/board" && me?.role === "sales" ? { ...i, label: "Client requests" } : i))
  ).map(label);
  // Client billing: sales and TLs manage/view it; the CEO and system admins too.
  // HR reaches it from the People group below instead.
  const canViewBilling = !!me && (me.role === "sales" || me.role === "tl" || me.role === "ceo" || me.is_admin);
  // Clients: sales and TLs manage them; the CEO and system admins view them.
  // (HR reaches them from the People group below; their Workspace is filtered.)
  const canViewClients = !!me && (me.is_admin || ["sales", "tl", "ceo"].includes(me.role));
  const workspaceExtra: NavItem[] = [];
  if (canViewBilling && me?.role !== "client") {
    workspaceExtra.push(
      { href: "/billing", label: "Billing", icon: ReceiptIcon },
      { href: "/payments", label: "Payments", icon: WalletIcon },
    );
  }
  if (canViewClients && me?.role !== "client") {
    workspaceExtra.push({ href: "/clients", label: "Clients", icon: Building2Icon });
  }
  // B2B: contracts, retainers and the B2B dashboard — sales/TL/CEO/admin in
  // Workspace; HR reaches them from the People group below.
  if (canViewClients && me?.role !== "client") {
    workspaceExtra.push(
      { href: "/contracts", label: "Contracts", icon: FileTextIcon },
      { href: "/retainers", label: "Retainers", icon: RepeatIcon },
      { href: "/b2b", label: "B2B", icon: HandshakeIcon },
    );
  }
  const workspaceFinal = [...workspace, ...workspaceExtra];
  const isClient = me?.role === "client";
  const isExternal = isClient || me?.role === "investor";
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <Link href="/">
                <div className="flex flex-col items-start gap-1 px-1 py-1">
                  <LogoMarkDark className="h-6" />
                  <span className="text-xs text-sidebar-foreground/60">Operations</span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <Suspense fallback={<NavGroup label="Workspace" items={workspaceFinal} />}>
          <ActiveNavGroup label="Workspace" items={workspaceFinal} />
        </Suspense>
        {!isExternal && (
          <Suspense fallback={<NavGroup label="People" items={hr} />}>
            <ActiveNavGroup label="People" items={hr} />
          </Suspense>
        )}
        {!isExternal && oversight.length > 0 && (
          <Suspense fallback={<NavGroup label="Oversight" items={oversight} />}>
            <ActiveNavGroup label="Oversight" items={oversight} />
          </Suspense>
        )}
        {!isExternal && admin.length > 0 && (
          <Suspense fallback={<NavGroup label="Admin" items={admin} />}>
            <ActiveNavGroup label="Admin" items={admin} />
          </Suspense>
        )}
      </SidebarContent>
      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

/** Approvals and team attendance show for HR, the CEO and anyone with reports. */
function useHrNav(signedIn: boolean): NavItem[] {
  const { data: me } = useMe();
  const directory = useDirectory();
  const hrMe = useHrMe(signedIn);
  const manages = !!me && (directory.data ?? []).some((p) => p.manager_id === me.id);
  const seesAll = me?.role === "ceo" || me?.role === "hr";
  const pending = hrMe.data?.approval_count ?? 0;
  const waiting = useInvoices({ status: "open" }, !!me?.permissions.includes("invoice.raise")).data?.waiting;
  const invoicesWaiting = me?.role === "ceo" ? waiting?.awaiting_ceo : me?.role === "hr" ? waiting?.pending_hr : 0;
  return [
    { href: "/hr", label: "My HR", icon: CalendarCheck2Icon },
    ...(manages || seesAll
      ? [{ href: "/hr/approvals", label: pending ? `Approvals (${pending})` : "Approvals", icon: InboxIcon }]
      : []),
    ...(manages || seesAll ? [{ href: "/hr/team", label: "Team attendance", icon: IdCardIcon }] : []),
    { href: "/hr/people", label: "People", icon: ContactIcon },
    // The org chart is for people who run teams; everyone else sees who they report to on the dashboard.
    ...(me && ["ceo", "hr", "tl"].includes(me.role) ? [{ href: "/hr/org", label: "Org chart", icon: NetworkIcon }] : []),
    ...(seesAll ? [{ href: "/hr/salaries", label: "Salaries", icon: WalletIcon }] : []),
    ...(seesAll ? [{ href: "/payroll", label: "Payroll", icon: BanknoteIcon }] : []),
    ...(seesAll ? [{ href: "/finance", label: "Finance", icon: LandmarkIcon }] : []),
    // Investors: capital partners, shares, distributions and withdrawals.
    ...(seesAll ? [{ href: "/investors", label: "Investors", icon: HandCoinsIcon }] : []),
    // Clients: HR views them here (sales/TL/CEO see them in Workspace).
    ...(me?.role === "hr" ? [{ href: "/clients", label: "Clients", icon: Building2Icon }] : []),
    // B2B: HR reads contracts, retainers and the dashboard here.
    ...(me?.role === "hr"
      ? [
          { href: "/contracts", label: "Contracts", icon: FileTextIcon },
          { href: "/retainers", label: "Retainers", icon: RepeatIcon },
          { href: "/b2b", label: "B2B", icon: HandshakeIcon },
        ]
      : []),
    // Client billing lives in Workspace for sales/TL/CEO; HR reaches it here.
    ...(me?.role === "hr" ? [{ href: "/billing", label: "Billing", icon: ReceiptIcon }] : []),
    // Milestone payments live in Workspace for sales/TL/CEO; HR reaches them here.
    ...(me?.role === "hr" ? [{ href: "/payments", label: "Payments", icon: WalletIcon }] : []),
    // Expenses start as invoices: the Office Admin raises them, HR and the CEO approve.
    ...(me?.permissions.includes("invoice.raise")
      ? [{ href: "/invoices", label: invoicesWaiting ? `Invoices (${invoicesWaiting})` : "Invoices", icon: ReceiptTextIcon }]
      : []),
  ];
}

function ActiveNavGroup(props: { label: string; items: NavItem[] }) {
  return <NavGroup {...props} pathname={usePathname()} />;
}

function NavGroup({ label, items, pathname }: { label: string; items: NavItem[]; pathname?: string }) {
  return (
    <SidebarGroup>
      <SidebarGroupLabel className="text-sidebar-foreground/50">{label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => {
            const active =
              !!pathname &&
              (item.href === "/" || item.href === "/hr" ? pathname === item.href : pathname.startsWith(item.href));
            return (
              <SidebarMenuItem key={item.href}>
                <SidebarMenuButton
                  asChild
                  isActive={active}
                  tooltip={item.label}
                  className="data-[active=true]:bg-sidebar-accent data-[active=true]:text-white data-[active=true]:[&>svg]:text-sidebar-primary"
                >
                  <Link href={item.href}>
                    <item.icon />
                    <span>{item.label}</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            );
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function NavUser() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const router = useRouter();

  async function logout() {
    await api("/auth/logout", { method: "POST" }).catch(() => {});
    qc.clear();
    router.replace("/login");
  }

  if (!me) {
    return (
      <div className="flex items-center gap-2 p-2">
        <Skeleton className="size-8 rounded-lg bg-sidebar-accent" />
        <Skeleton className="h-4 flex-1 bg-sidebar-accent" />
      </div>
    );
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton size="lg" className="data-[state=open]:bg-sidebar-accent">
              <Avatar className="size-8 rounded-lg">
                <AvatarFallback className="rounded-lg bg-sidebar-primary font-medium text-sidebar-primary-foreground">
                  {initials(me.name)}
                </AvatarFallback>
              </Avatar>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium text-white">{me.name}</span>
                <span className="truncate text-xs text-sidebar-foreground/60">{ROLE_LABELS[me.role]}</span>
              </div>
              <ChevronsUpDownIcon className="ml-auto size-4" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent side="right" align="end" sideOffset={8} className="w-60">
            <DropdownMenuLabel className="font-normal">
              <div className="grid text-sm leading-tight">
                <span className="font-medium">{me.name}</span>
                <span className="text-xs text-muted-foreground">{me.email}</span>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link href="/account">
                <UserRoundIcon /> Account
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={logout}>
              <LogOutIcon /> Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
