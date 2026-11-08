import type { ActivityRow } from "./reports";

export type Role = "sales" | "tl" | "member" | "ceo" | "hr" | "office_admin" | "client" | "investor";

export const ROLE_LABELS: Record<Role, string> = {
  sales: "Sales",
  tl: "Ops Team Leader",
  member: "Ops Team Member",
  ceo: "CEO",
  hr: "HR",
  office_admin: "Office Admin",
  client: "Client",
  investor: "Investor",
};

/** Roles with no project access at all: they get their own home and no project pages. */
export type ProjectRole = Exclude<Role, "hr" | "office_admin" | "client" | "investor">;

export function projectRoleOf(role: Role | undefined): ProjectRole | undefined {
  return role && role !== "hr" && role !== "office_admin" && role !== "client" && role !== "investor" ? role : undefined;
}

export type User = {
  id: string;
  email: string;
  name: string;
  role: Role;
  is_admin: boolean;
  /** Opens the Analytics page to someone other than the CEO (granted by the CEO). */
  can_view_analytics: boolean;
  is_active: boolean;
  last_login_at: string | null;
  created_at: string;
  updated_at: string;
};

export type Me = User & { permissions: string[] };

export type Profile = {
  id: string;
  name: string;
  platform: string;
  /** The marketplace's share of each order, in percent (Fiverr: 20). */
  fee_percent: number | string;
  is_active: boolean;
  created_at: string;
};

export type ProjectType = {
  id: string;
  name: string;
  is_active: boolean;
  created_at: string;
  team_id: string | null; // projects of this type are routed to this team
};

export type Team = {
  id: string;
  name: string;
  lead_id: string | null;
  lead_name: string | null;
  department_id: string | null;
  department_name: string | null;
  is_active: boolean;
  created_at: string;
  member_count: number;
  project_types: string[];
};

export type TeamMember = { id: string; name: string; role: Role; is_active: boolean; team_id: string; added_at: string };

export type Settings = {
  inbox_requires_approval: boolean;
  org_timezone: string;
  batch_window_minutes: number;
  work_start: string;
  work_end: string;
  late_grace_minutes: number;
  weekly_off_days: number[];
};

// The Fiverr-style flow: New → In Progress → In Review (TL check) → Ready to Deliver →
// Delivered (Sales, on the marketplace) → Completed, plus Blocked and Cancelled.
export type ProjectStatus =
  | "new"
  | "in_progress"
  | "blocked"
  | "ready_for_review"
  | "ready_to_deliver"
  | "delivered"
  | "completed"
  | "cancelled";

/** Statuses from the old flow still appear in project history. */
export type HistoryStatus = ProjectStatus | "draft" | "submitted" | "needs_info" | "assigned";

export const STATUS_LABELS: Record<HistoryStatus, string> = {
  new: "New",
  in_progress: "In Progress",
  blocked: "Blocked",
  ready_for_review: "In Review",
  ready_to_deliver: "Ready to Deliver",
  delivered: "Delivered",
  completed: "Completed",
  cancelled: "Cancelled",
  draft: "Draft",
  submitted: "Submitted",
  needs_info: "Needs Info",
  assigned: "Assigned",
};

export const STATUSES: ProjectStatus[] = [
  "new",
  "in_progress",
  "blocked",
  "ready_for_review",
  "ready_to_deliver",
  "delivered",
  "completed",
  "cancelled",
];

export type Priority = "low" | "medium" | "high" | "urgent";
export const PRIORITIES: Priority[] = ["low", "medium", "high", "urgent"];
export const PRIORITY_LABELS: Record<Priority, string> = { low: "Low", medium: "Medium", high: "High", urgent: "Urgent" };

export type Project = {
  id: string;
  number: number;
  name: string | null;
  profile_id: string | null;
  profile_name: string | null;
  client_name: string | null;
  order_id: string | null;
  client_contact: string | null;
  project_type_id: string | null;
  project_type_name: string | null;
  team_id: string | null;
  team_name: string | null;
  member_ids: string[]; // everyone assigned; assigned_to is the first of them
  member_names: string[];
  scope: string | null;
  /** The marketplace's share recorded on this project, and what it leaves Growvence. Empty until there's an agreed price. */
  platform_fee_percent: string;
  platform_fee: string;
  net_amount: string;
  /** Access to the client's store: "not_requested" is the one to chase. */
  store_access: StoreAccess;
  store_access_link: string | null;
  store_access_changed_at: string | null;
  agreed_price: string | null;
  currency: string;
  start_date: string | null;
  deadline: string | null;
  priority: Priority;
  special_notes: string | null;
  status: ProjectStatus;
  created_by: string;
  created_by_name: string;
  assigned_to: string | null;
  assigned_to_name: string | null;
  due_date: string | null;
  effective_due: string | null;
  revision_count: number;
  first_submitted_at: string | null;
  first_assigned_at: string | null;
  completed_at: string | null;
  cancelled_at: string | null;
  last_activity_at: string;
  lock_version: number;
  created_at: string;
  updated_at: string;
  is_overdue: boolean;
  has_updates?: boolean;
};

export type ProjectList = {
  projects: Project[];
  total: number;
  counts: Partial<Record<ProjectStatus, number>>;
  page: number;
  page_size: number;
};

export type WorkflowAction = {
  from: ProjectStatus;
  to: ProjectStatus;
  label: string;
  needs_comment: boolean;
  revision: boolean;
};

export type ProjectDetail = {
  project: Project;
  permissions: {
    can_edit: boolean;
    can_set_priority: boolean;
    can_assign: boolean;
    can_move_team: boolean;
    can_plan: boolean;
    can_comment: boolean;
    can_upload: boolean;
    can_remove_files: boolean; // any file; otherwise only your own uploads
    can_acknowledge: boolean;
    can_set_access: boolean;
  };
  actions: WorkflowAction[];
  history: {
    id: number;
    from_status: HistoryStatus | null;
    to_status: HistoryStatus;
    comment: string | null;
    created_at: string;
    changed_by: string;
    changed_by_name: string;
  }[];
  assignments: {
    id: string;
    user_id: string;
    user_name: string;
    assigned_by_name: string;
    priority: Priority;
    due_date: string | null;
    assigned_at: string;
    unassigned_at: string | null;
  }[];
  comments: {
    id: string;
    body: string;
    created_at: string;
    author_id: string;
    author_name: string;
    author_role: Role;
  }[];
  changes: {
    id: string;
    field: string;
    old_value: string | null;
    new_value: string | null;
    is_important: boolean;
    created_at: string;
    changed_by: string;
    changed_by_name: string;
  }[];
  files: ProjectFile[];
  versions: FileVersion[];
  acknowledgements: Acknowledgement[];
  pending_acks: number;
};

export type ProjectFile = {
  id: string;
  original_name: string;
  created_at: string;
  uploaded_by: string;
  uploaded_by_name: string;
  version_id: string;
  version_no: number;
  size_bytes: number;
  mime_type: string;
  version_created_at: string;
  version_uploaded_by_name: string;
  version_count: number;
};

export type FileVersion = {
  id: string;
  file_id: string;
  version_no: number;
  size_bytes: number;
  mime_type: string;
  sha256: string;
  created_at: string;
  uploaded_by: string;
  uploaded_by_name: string;
};

export type Acknowledgement = {
  id: string;
  project_change_id: string;
  user_id: string;
  user_name: string;
  acknowledged_at: string | null;
  created_at: string;
};

export type DirectoryUser = { id: string; name: string; role: Role; team_id: string | null };

export type UpdateStatus = "draft" | "pending_approval" | "changes_requested" | "approved" | "sent";
export const UPDATE_STATUS_LABELS: Record<UpdateStatus, string> = {
  draft: "Draft",
  pending_approval: "Pending Approval",
  changes_requested: "Changes Requested",
  approved: "Approved",
  sent: "Sent",
};

export type Destination = "inbox" | "order_page" | "delivery";
export const DESTINATIONS: Destination[] = ["inbox", "order_page", "delivery"];
export const DESTINATION_LABELS: Record<Destination, string> = { inbox: "Inbox", order_page: "Order page", delivery: "Delivery" };
export const DESTINATION_HINTS: Record<Destination, string> = {
  inbox: "Chat with the client",
  order_page: "The order's activity / notes section",
  delivery: "The delivery section (needs an attachment)",
};

export type Update = {
  id: string;
  number: number;
  /** Set on a project's update; client_request_id on a client with no project. */
  project_id: string | null;
  client_request_id: string | null;
  project_number: number;
  project_name: string | null;
  order_id: string | null;
  profile_name: string | null;
  client_name: string | null;
  project_status: ProjectStatus;
  project_sales_id: string;
  project_assignee_id: string | null;
  author_id: string;
  author_name: string;
  message: string;
  destination: Destination;
  status: UpdateStatus;
  revision_number: number | null;
  submitted_at: string | null;
  checked_by: string | null;
  checked_by_name: string | null;
  checked_at: string | null;
  auto_approved: boolean;
  sent_by: string | null;
  sent_by_name: string | null;
  sent_at: string | null;
  lock_version: number;
  created_at: string;
  updated_at: string;
  attachment_count: number;
  ops_comment_count: number;
  sales_comment_count: number;
};

export type UpdateList = {
  updates: Update[];
  total: number;
  counts: Partial<Record<UpdateStatus, number>>;
  page: number;
  page_size: number;
};

export type UpdateDetail = {
  update: Update;
  permissions: {
    can_edit: boolean;
    can_submit: boolean;
    can_approve: boolean;
    can_request_changes: boolean;
    can_set_destination: boolean;
    can_mark_sent: boolean;
    can_comment_ops: boolean;
    can_comment_sales: boolean;
    can_read_ops: boolean;
  };
  attachments: {
    id: string;
    original_name: string;
    created_at: string;
    uploaded_by: string;
    uploaded_by_name: string;
    version_id: string;
    version_no: number;
    size_bytes: number;
    mime_type: string;
  }[];
  comments: {
    id: string;
    thread: "ops" | "sales";
    body: string;
    created_at: string;
    author_id: string;
    author_name: string;
    author_role: Role;
  }[];
  history: {
    id: number;
    from_status: UpdateStatus | null;
    to_status: UpdateStatus;
    comment: string | null;
    created_at: string;
    changed_by: string;
    changed_by_name: string;
  }[];
  needs_approval: boolean;
};

export type CardColumn = "todo" | "in_progress" | "review" | "done";
export const CARD_COLUMNS: CardColumn[] = ["todo", "in_progress", "review", "done"];
export const CARD_COLUMN_LABELS: Record<CardColumn, string> = {
  todo: "To do",
  in_progress: "In progress",
  review: "Review",
  done: "Done",
};

export type Card = {
  id: string;
  /** A card hangs on a project or on a client request; container_id is whichever it is. */
  project_id: string | null;
  client_request_id: string | null;
  container_id: string;
  number: number;
  title: string;
  description: string;
  source: "client" | "internal";
  column_key: CardColumn;
  position: number;
  /** Shared with the linked client (comments, approvals) when true. */
  visible_to_client: boolean;
  due_date: string | null;
  created_by: string;
  created_by_name: string;
  created_at: string;
  updated_at: string;
  done_at: string | null;
  assignee_ids: string[];
  assignee_names: string[];
  comment_count: number;
  attachment_count: number;
  can_edit: boolean;
  can_remove: boolean;
  can_comment: boolean;
};

export type Board = {
  cards: Card[];
  columns: CardColumn[];
  can_create: boolean;
  /** Drag cards between columns. On a client request the whole team may. */
  can_move: boolean;
  /** Hand a card to someone — the Team Leader's, or a member already working it. */
  can_assign: boolean;
  own_only: boolean;
};

export type CardComment = { id: string; body: string; created_at: string; author_id: string; author_name: string; author_role: Role };
export type CardAttachment = {
  id: string;
  original_name: string;
  created_at: string;
  uploaded_by: string;
  uploaded_by_name: string;
  version_id: string;
  size_bytes: number;
  mime_type: string;
};
export type CardView = { card: Card; comments: CardComment[]; attachments: CardAttachment[]; can_attach: boolean };

/** A project on the all-projects board: its members and what the viewer may do there. */
export type BoardProject = {
  id: string;
  number: number;
  name: string | null;
  status: ProjectStatus;
  team_name: string | null;
  client_name: string | null;
  client_contact: string | null; // usually the buyer's marketplace username
  order_id: string | null;
  profile_name: string | null;
  agreed_price: string | null;
  currency: string;
  member_ids: string[];
  member_names: string[];
  can_create: boolean;
  can_move: boolean;
  can_assign: boolean;
};

export type AllCard = Card & { project_number: number; project_name: string | null; project_team_name: string | null };
export type AllCards = {
  cards: AllCard[];
  projects: BoardProject[];
  requests: BoardRequest[];
  columns: CardColumn[];
  own_only: boolean;
};

export type RequestStatus = "open" | "done" | "cancelled";
export const REQUEST_STATUS_LABELS: Record<RequestStatus, string> = { open: "Open", done: "Done", cancelled: "Cancelled" };

/** A client request on the all-projects board, alongside the projects. */
export type BoardRequest = {
  id: string;
  number: number;
  title: string;
  status: RequestStatus;
  team_name: string;
  client_name: string;
  order_id: string | null;
  profile_name: string;
  member_ids: string[];
  member_names: string[];
  can_create: boolean;
  can_move: boolean;
  can_assign: boolean;
};

/**
 * A client request: work for a client with no project in the portal — one on monthly
 * management, or an order delivered before the portal existed. Sales raise it and send
 * its approved updates; the team it is sent to works its cards and writes them.
 */
export type ClientRequest = {
  id: string;
  number: number;
  profile_id: string;
  profile_name: string;
  client_name: string;
  order_id: string | null;
  title: string;
  details: string;
  status: RequestStatus;
  status_label: string;
  team_id: string;
  team_name: string;
  team_lead_id: string | null;
  team_head_id: string | null;
  created_by: string;
  created_by_name: string;
  closed_by: string | null;
  closed_by_name: string | null;
  closed_at: string | null;
  last_activity_at: string;
  created_at: string;
  updated_at: string;
  member_ids: string[];
  member_names: string[];
  card_count: number;
  done_card_count: number;
  update_count: number;
  unsent_update_count: number;
};

export type RequestPermissions = {
  can_edit: boolean;
  can_move_team: boolean;
  can_close: boolean;
  can_cancel: boolean;
  can_reopen: boolean;
  can_add_card: boolean;
  can_write_note: boolean;
  can_assign: boolean;
};

export type RequestDetail = {
  request: ClientRequest;
  permissions: RequestPermissions;
  cards: Card[];
  columns: CardColumn[];
  roster: { id: string; name: string; role: Role; is_active: boolean }[];
  activity: ActivityRow[];
  can_move_card: boolean;
};

export type RequestList = {
  requests: ClientRequest[];
  total: number;
  counts: Partial<Record<RequestStatus, number>>;
  page: number;
  page_size: number;
  can_raise: boolean;
};

export type StoreAccess = "not_requested" | "requested" | "given";
export const STORE_ACCESS_LABELS: Record<StoreAccess, string> = {
  not_requested: "Not requested",
  requested: "Requested",
  given: "Access given",
};
