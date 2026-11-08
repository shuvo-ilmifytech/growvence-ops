// End-to-end QA: walks the whole spec in a real browser as every role.
//
//   BASE_URL=http://localhost:3000 CHROME_PATH=/usr/bin/chromium node qa.mjs
//
// Needs the demo users from `opsctl seed-dev` (password123!) and creates its own
// uniquely-named data, so it can run repeatedly against the same database.
import { chromium } from "playwright-core";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const BASE = process.env.BASE_URL ?? "http://localhost:3000";
const CHROME = process.env.CHROME_PATH ?? "/usr/bin/chromium";
const SHOTS = process.env.SHOTS_DIR; // optional: save screenshots here
const PASSWORD = process.env.QA_PASSWORD ?? "password123!";
const tag = "QA" + Date.now().toString().slice(-6);

const results = [];
const consoleErrors = [];
let section = "";

function check(name, ok, detail = "") {
  results.push({ section, name, ok: !!ok, detail });
  console.log(`${ok ? "  ✓" : "  ✗"} ${name}${detail ? ` (${detail})` : ""}`);
}
async function step(name, fn) {
  try {
    await fn();
    check(name, true);
  } catch (err) {
    check(name, false, String(err.message ?? err).split("\n")[0].slice(0, 160));
  }
}
function begin(s) {
  section = s;
  console.log(`\n${s}`);
}

const browser = await chromium.launch({
  executablePath: CHROME,
  // A fake microphone, auto-granted, so a real voice call can be placed between two tabs.
  args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream", "--autoplay-policy=no-user-gesture-required"],
});
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "opsflow-qa-"));
fs.writeFileSync(path.join(tmp, "brief.txt"), "Brief: homepage, 3 collections.\n");
fs.writeFileSync(path.join(tmp, "brief-v2.txt"), "Brief v2: homepage, 4 collections and a blog.\n");
fs.writeFileSync(path.join(tmp, "delivery.zip"), "final files");
fs.writeFileSync(path.join(tmp, "client-logo.png"), "logo bytes from the client");

async function session(who, viewport = { width: 1440, height: 900 }) {
  const ctx = await browser.newContext({
    viewport,
    acceptDownloads: true,
    permissions: ["clipboard-read", "clipboard-write", "microphone"],
  });
  const page = await ctx.newPage();
  page.on("console", (m) => {
    if (m.type() === "error" && !m.text().includes("Failed to load resource")) consoleErrors.push(`${who}: ${m.text().split("\n")[0].slice(0, 200)}`);
  });
  await page.goto(BASE + "/login");
  await page.fill("#email", `${who}@opsflow.local`);
  await page.fill("#password", PASSWORD);
  await page.click("button[type=submit]");
  await page.waitForURL(BASE + "/");
  await page.waitForLoadState("networkidle");
  return page;
}
// API calls from inside a signed-in page (same cookie, same origin), for setup and permission probes.
const api = (page, method, p, body) =>
  page.evaluate(
    async ([method, p, body]) => {
      const r = await fetch("/api" + p, { method, headers: { "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
      let json = null;
      try {
        json = await r.json();
      } catch {}
      return { status: r.status, json };
    },
    [method, p, body],
  );
// How many of one kind of alert this person has, and a wait for it to reach a number.
const alertCount = async (page, kind) =>
  ((await api(page, "GET", "/alerts")).json.items.find((i) => i.kind === kind)?.count ?? 0);
const waitForAlert = async (page, kind, want) => {
  for (let i = 0; i < 20; i++) {
    if ((await alertCount(page, kind)) === want) return;
    await page.waitForTimeout(500);
  }
  throw new Error(`${kind} stayed at ${await alertCount(page, kind)}, wanted ${want}`);
};

const shot = async (page, name, fullPage = false) => SHOTS && page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage });
const header = (page) => page.locator("[data-slot=sheet-header]");
const sheet = (page) => page.getByRole("dialog");
const pick = async (page, trigger, option) => {
  await trigger.click();
  await page.getByRole("option", { name: option, exact: true }).click();
};

// ── 1. Sign-in and navigation ─────────────────────────────────────────
begin("1. Sign-in and role navigation");
{
  const ctx = await browser.newContext();
  const p = await ctx.newPage();
  await step("protected page redirects to sign-in", async () => {
    await p.goto(BASE + "/projects");
    await p.waitForURL("**/login");
  });
  await step("wrong password is rejected with a clear message", async () => {
    await p.fill("#email", "sales@opsflow.local");
    await p.fill("#password", "wrong-password");
    await p.click("button[type=submit]");
    await p.getByText("Email or password is incorrect").waitFor();
  });
  await ctx.close();
}
const sales = await session("sales");
const tl = await session("tl");
const member = await session("member");
const mgmt = await session("ceo"); // the CEO (formerly Management)
const hr = await session("hr");
const admin = await session("admin");
// Everyone's id, for the API calls these checks make directly.
const e2eIds = Object.fromEntries(
  (await api(tl, "GET", "/users/directory")).json.users.map((u) => [u.name.split(" ")[0].toLowerCase(), u.user_id ?? u.id]),
);
const navOf = async (page) => (await page.locator("[data-sidebar=content]").innerText()).split("\n").filter(Boolean);
const invoiceCard = (page, what) => page.locator("[data-slot=card]").filter({ hasText: what });
const expectNav = {
  sales: ["Dashboard", "My work", "Projects", "Updates", "Client requests"],
  tl: ["Dashboard", "My work", "Projects", "Updates", "Board", "Activity", "Org chart"],
  member: ["Dashboard", "My work", "Projects", "Updates", "Board"],
  mgmt: ["Dashboard", "My work", "Projects", "Updates", "Board", "Reports", "Activity", "Users", "Teams", "Org chart", "Profiles & types"],
  hr: ["Dashboard", "My HR", "Approvals", "People", "Org chart", "Users", "Teams", "HR settings"],
};
for (const [who, page] of [["sales", sales], ["tl", tl], ["member", member], ["mgmt", mgmt], ["hr", hr]]) {
  const nav = await navOf(page);
  const missing = expectNav[who].filter((n) => !nav.includes(n));
  const adminLeak = !["mgmt", "hr"].includes(who) && (nav.includes("Users") || nav.includes("Settings"));
  const projectLeak = who === "hr" && (nav.includes("Projects") || nav.includes("Updates") || nav.includes("My work"));
  // The org chart is for the CEO, HR and Team Leaders; members and Sales see their manager on the dashboard.
  const orgLeak = ["sales", "member"].includes(who) && nav.includes("Org chart");
  check(`${who} sees the right navigation`, !missing.length && !adminLeak && !projectLeak && !orgLeak && !(who !== "mgmt" && nav.includes("Reports")), nav.join(", "));
}
{
  const nav = await navOf(admin);
  check("admin flag gives system setup, not people", nav.includes("Profiles & types") && !nav.includes("Users"), nav.join(", "));
}
for (const p of [sales, tl, member, mgmt]) await api(p, "POST", "/notifications/read-all");

// ── 2. Sales creates a project (it goes straight to the team) ─────────
begin("2. Project creation (Sales)");
let projectId;
const projectName = `Storefront ${tag}`;
await step("creating with missing fields shows highlighted errors", async () => {
  await sales.goto(BASE + "/projects/new");
  await sales.fill("#name", projectName);
  await sales.getByRole("button", { name: "Create project" }).click();
  await sales.getByText("Please fill in or fix the highlighted fields").waitFor();
});
await step("fill the form and create it", async () => {
  await pick(sales, sales.locator("#profile_id"), (await api(sales, "GET", "/profiles")).json.profiles[0].name + ((await api(sales, "GET", "/profiles")).json.profiles[0].platform ? " · " + (await api(sales, "GET", "/profiles")).json.profiles[0].platform : ""));
  await sales.fill("#order_id", "FO-" + tag);
  await sales.fill("#client_name", "Client " + tag);
  await sales.fill("#client_contact", "buyer_" + tag);
  // Shopify Store is routed to the Shopify team (Tara leads, Milo is a member) by the seed.
  await pick(sales, sales.locator("#project_type_id"), "Shopify Store");
  await sales.fill("#scope", "Shopify store with 3 collections and a custom homepage.");
  await sales.fill("#agreed_price", "1250");
  const d = (n) => new Date(Date.now() + n * 864e5).toISOString().slice(0, 10);
  // The delivery deadline carries a time.
  await sales.fill("#deadline", `${d(25)}T17:30`);
  await sales.getByRole("button", { name: "Create project" }).click();
  await sales.waitForURL(/\/projects\/[0-9a-f-]{36}$/);
  projectId = sales.url().split("/").pop();
});
const projectURL = `${BASE}/projects/${projectId}`;
await step("TL is notified in real time (toast + bell)", async () => {
  await tl.getByText(new RegExp(`New project to assign: #\\d+ ${projectName}`)).first().waitFor({ timeout: 8000 });
});
await step("the project lands with the Shopify team only", async () => {
  const r = await api(sales, "GET", `/projects/${projectId}`);
  if (r.json.project.team_name !== "Shopify") throw new Error("team " + r.json.project.team_name);
  const other = await api(admin, "GET", `/projects/${projectId}`); // Ava leads Web Dev
  if (other.status !== 404) throw new Error("Web Dev lead got " + other.status);
  const n = await api(admin, "GET", "/notifications?limit=50");
  if (n.json.notifications.some((x) => x.project_id === projectId)) throw new Error("Web Dev lead was notified");
});
await step("Sales uploads a file, then a new version", async () => {
  await sales.goto(projectURL);
  await sales.waitForLoadState("networkidle");
  await sales.locator("input[type=file][multiple]").setInputFiles(path.join(tmp, "brief.txt"));
  await sales.getByText("brief.txt").first().waitFor();
  await sales.getByRole("button", { name: "More for brief.txt" }).click();
  await sales.getByRole("menuitem", { name: "Upload new version" }).click();
  await sales.locator("input[type=file]:not([multiple])").setInputFiles(path.join(tmp, "brief-v2.txt"));
  await sales.getByText("2 versions").waitFor();
});

// ── 3. TL assigns, which starts the project ─────────────────────────────
begin("3. Assignment (TL)");
await step("Sales sees it waiting for the Team Leader", async () => {
  await sales.goto(projectURL);
  await sales.getByText("Waiting for the Team Leader to assign it").waitFor();
});
await step("assign list offers only the Shopify team's members", async () => {
  await tl.goto(projectURL);
  await tl.getByText("Shopify", { exact: true }).first().waitFor();
  await tl.getByRole("button", { name: "Assign", exact: true }).click();
  const options = await sheet(tl).locator("button[aria-pressed]").allInnerTexts();
  await tl.keyboard.press("Escape");
  // The leader can take it on too (8.4.5).
  if (options.join() !== "Tara Lead (me),Milo Member,Omar Member") throw new Error(options.join(", "));
  const r = await api(tl, "POST", `/projects/${projectId}/assign`, { assignee_ids: [(await api(tl, "GET", "/users/directory?role=member")).json.users.find((u) => u.name === "Mina Member").id], priority: "low" });
  if (r.status !== 422) throw new Error("assigning Mina (Web Dev) got " + r.status);
});
await step("TL assigns to Milo with priority and due date", async () => {
  await tl.reload();
  await tl.getByRole("button", { name: "Assign", exact: true }).click();
  await sheet(tl).getByRole("button", { name: "Milo Member" }).click();
  await sheet(tl).getByRole("button", { name: "Assign", exact: true }).click();
  await tl.getByText("assigned it to Milo Member").waitFor();
  await tl.getByText("In Progress").first().waitFor();
  await member.getByText(/You've been assigned/).first().waitFor({ timeout: 8000 });
});

// ── 4. Sales edits after assignment → acknowledgement ─────────────────
begin("4. Sales edits, batching and acknowledgement");
await step("important change (deadline) asks the member to acknowledge", async () => {
  const d = new Date(Date.now() + 18 * 864e5).toISOString().slice(0, 10);
  await sales.goto(projectURL + "/edit");
  await sales.fill("#deadline", `${d}T09:15`);
  await sales.fill("#client_contact", "ops_" + tag);
  await sales.getByRole("button", { name: "Save changes" }).click();
  await sales.waitForURL(projectURL);
  await member.getByText(/Please acknowledge/).first().waitFor({ timeout: 8000 });
});
await step("the delivery deadline keeps its time", async () => {
  const dl = (await api(sales, "GET", `/projects/${projectId}`)).json.project.deadline;
  const local = new Date(dl);
  if (`${String(local.getHours()).padStart(2, "0")}:${String(local.getMinutes()).padStart(2, "0")}` !== "09:15") {
    throw new Error("deadline lost its time: " + dl);
  }
  await sales.goto(projectURL);
  await sales.getByText("Delivery deadline").waitFor();
});
await step("member acknowledges from the banner; TL sees it", async () => {
  await member.goto(projectURL);
  await member.getByText("Sales changed important details").waitFor();
  await member.getByRole("button", { name: /Acknowledge/ }).click();
  await member.getByText("Acknowledged by Milo Member").waitFor();
  await tl.goto(projectURL);
  await tl.getByText("Acknowledged by Milo Member").waitFor();
});
await step("non-important edits are batched into one notification", async () => {
  const r = await api(member, "GET", "/notifications?limit=50");
  const batches = r.json.notifications.filter((n) => n.type === "project.changes" && n.project_id === projectId);
  if (batches.length !== 1 || !batches[0].body.includes("Client contact changed")) throw new Error(JSON.stringify(batches.map((b) => b.body)));
});

// ── 5. Work and updates ───────────────────────────────────────────────
begin("5. Work and client updates");
await step("member blocks with a reason, unblocks", async () => {
  await member.goto(projectURL);
  await member.getByRole("button", { name: "Mark blocked" }).click();
  await sheet(member).locator("textarea").fill("Waiting for the logo files");
  await sheet(member).getByRole("button", { name: "Mark blocked" }).click();
  await tl.getByText(/is blocked/).first().waitFor({ timeout: 8000 });
  await member.getByRole("button", { name: "Unblock" }).click();
  await member.getByRole("button", { name: "Send for delivery" }).waitFor();
});
let updateId;
await step("member writes an Order page update and submits it", async () => {
  await member.getByRole("button", { name: "New update" }).click();
  await header(member).getByText("UPD-").waitFor();
  await sheet(member).locator("textarea").first().fill(`Homepage draft is ready (${tag}).`);
  await pick(member, sheet(member).getByRole("combobox").first(), "Order page · The order's activity / notes section");
  await member.getByText("Destination set to Order page").waitFor();
  await sheet(member).getByRole("button", { name: "Submit for approval" }).click();
  await header(member).getByText("Pending Approval").waitFor();
  updateId = new URL(member.url()).searchParams.get("open") ?? (await api(member, "GET", `/updates?project_id=${projectId}`)).json.updates[0].id;
  await member.keyboard.press("Escape");
});
await step("Sales cannot see the pending update (API 404)", async () => {
  const r = await api(sales, "GET", `/updates/${updateId}`);
  if (r.status !== 404) throw new Error("status " + r.status);
});
await step("TL requests changes; member resubmits; TL approves", async () => {
  await tl.goto(BASE + "/updates?view=approve");
  await tl.locator("tbody tr", { hasText: tag }).click();
  await sheet(tl).getByRole("button", { name: "Request changes" }).click();
  await tl.getByPlaceholder("What should change?").fill("Add the staging link.");
  await tl.getByRole("button", { name: "Send back" }).click();
  await header(tl).getByText("Changes Requested").waitFor();
  await member.goto(`${BASE}/updates?view=all&open=${updateId}`);
  await sheet(member).getByRole("tabpanel").getByText("Add the staging link.").waitFor();
  await sheet(member).locator("textarea").first().fill(`Homepage draft is ready (${tag}): https://staging.example`);
  await sheet(member).getByRole("button", { name: "Resubmit for approval" }).click();
  await header(member).getByText("Pending Approval").waitFor();
  await tl.goto(`${BASE}/updates?view=all&open=${updateId}`);
  await sheet(tl).getByRole("button", { name: "Approve" }).click();
  await header(tl).getByText("Approved", { exact: true }).waitFor();
});
await step("the approved update rings the Sales browser until it is sent", async () => {
  await waitForAlert(sales, "unsent_updates", 1);
  await sales.goto(BASE + "/");
  await sales.waitForLoadState("networkidle");
  const card = sales.getByRole("status").filter({ hasText: "waiting to be sent" });
  await card.waitFor({ timeout: 10000 });
  // The tab title carries the count too, for a background tab.
  await sales.waitForFunction(() => /^\(\d+\)/.test(document.title), null, { timeout: 10000 });
  // Snoozing says so plainly, so nobody wonders whether it broke.
  await sales.getByRole("button", { name: "Snooze 15 min" }).click();
  await sales.getByText(/quiet for \d+ more min/).waitFor();
  // Nobody else is rung about sending updates.
  if ((await alertCount(tl, "unsent_updates")) !== 0) throw new Error("the Team Leader is rung about sending updates");
  if ((await alertCount(member, "unsent_updates")) !== 0) throw new Error("a member is rung about sending updates");
});

await step("Sales sees it ready to send, never the Ops thread, and marks it sent", async () => {
  await sales.goto(`${BASE}/updates?view=send&open=${updateId}`);
  await header(sales).getByText("Approved", { exact: true }).waitFor();
  if (await sheet(sales).getByRole("tab", { name: /Ops thread/ }).count()) throw new Error("Ops thread visible to Sales");
  await sheet(sales).getByRole("button", { name: "Mark as sent" }).click();
  await header(sales).getByText("Sent", { exact: true }).waitFor();
  await member.getByText(/was sent to the client/).first().waitFor({ timeout: 8000 });
});

await step("sending it stops the ringing", async () => {
  await waitForAlert(sales, "unsent_updates", 0);
  await sales.goto(BASE + "/");
  await sales.waitForLoadState("networkidle");
  if (await sales.getByRole("status").filter({ hasText: "waiting to be sent" }).count()) {
    throw new Error("still ringing after the update was sent");
  }
  if (/^\(\d+\)/.test(await sales.title())) throw new Error("the tab title still shows a count");
});

// ── 6. Several members and the project board ──────────────────────────
begin("6. Members and the project board");
const omar = await session("member3");
const cardByTitle = async (page, title) => (await api(page, "GET", `/projects/${projectId}/cards`)).json.cards.find((c) => c.title === title);
const request = `Navy header ${tag}`, task = `Build collections ${tag}`;
await step("TL adds Omar; both members see the project", async () => {
  await tl.goto(projectURL);
  await tl.getByRole("button", { name: "Members", exact: true }).click();
  await sheet(tl).getByRole("button", { name: "Omar Member" }).click();
  await sheet(tl).getByRole("button", { name: "Save members" }).click();
  await tl.getByText(/Members updated: Milo Member, Omar Member/).waitFor();
  const r = await api(omar, "GET", `/projects/${projectId}`);
  if (r.status !== 200 || r.json.project.status === "assigned") throw new Error(`omar ${r.status}, status ${r.json?.project?.status} (should keep its status)`);
});
await step("Sales adds a client request; members are told", async () => {
  await sales.goto(projectURL);
  await sales.getByRole("tab", { name: /Client requests/ }).click();
  await sales.getByRole("button", { name: "Add client request" }).click();
  await sales.getByRole("textbox", { name: "Request" }).fill(request);
  await sales.getByRole("textbox", { name: "Details" }).fill("Client wants the header navy like the logo.");
  await sales.getByRole("button", { name: "Add", exact: true }).click();
  await sales.getByText("Request added").waitFor();
  await omar.getByText(new RegExp(`New client request on .*${request}`)).first().waitFor({ timeout: 8000 });
});
await step("TL adds a task for Omar", async () => {
  await tl.goto(projectURL + "?tab=board");
  await tl.getByRole("button", { name: "Add task" }).click();
  await tl.getByRole("textbox", { name: "Task" }).fill(task);
  await sheet(tl).getByRole("button", { name: "Omar Member" }).click();
  await tl.getByRole("button", { name: "Add", exact: true }).click();
  await omar.getByText(new RegExp(`gave you a card: ${task}`)).first().waitFor({ timeout: 8000 });
});
await step("Omar's new task rings his browser; opening it stops that", async () => {
  await waitForAlert(omar, "unopened_cards", 1);
  await omar.goto(BASE + "/my-work");
  await omar.waitForLoadState("networkidle");
  const card = omar.getByRole("status").filter({ hasText: "you haven't opened yet" });
  await card.waitFor({ timeout: 10000 });
  await omar.waitForFunction(() => /^\(\d+\)/.test(document.title), null, { timeout: 10000 });
  // Nobody else is rung about Omar's card.
  if ((await alertCount(member, "unopened_cards")) !== 0) throw new Error("Milo is rung about Omar's card");
  // Opening the card is what stops it.
  const c = await cardByTitle(omar, task);
  await omar.goto(`${projectURL}?tab=board&card=${c.id}`);
  await sheet(omar).getByText(task).first().waitFor();
  await omar.keyboard.press("Escape");
  await waitForAlert(omar, "unopened_cards", 0);
  await card.waitFor({ state: "detached", timeout: 15000 });
});

await step("Milo drags the client request to In progress", async () => {
  await member.goto(projectURL + "?tab=board");
  const card = member.getByRole("button", { name: new RegExp(request) });
  await card.waitFor();
  const a = await card.boundingBox(), b = await member.getByRole("region", { name: "In progress" }).boundingBox();
  await member.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await member.mouse.down();
  await member.mouse.move(a.x + a.width / 2 + 20, a.y + a.height / 2, { steps: 5 });
  await member.mouse.move(b.x + b.width / 2, b.y + 60, { steps: 15 });
  await member.mouse.up();
  await member.getByText("Moved to In progress").waitFor();
  if ((await cardByTitle(member, request)).column_key !== "in_progress") throw new Error("not moved on the server");
});
await step("Omar comments and sends his task to Review; the TL is told", async () => {
  await omar.goto(projectURL + "?tab=board");
  await omar.getByRole("button", { name: new RegExp(task) }).click();
  await omar.getByRole("textbox", { name: "Comment" }).fill("4 collections set up");
  await omar.getByRole("button", { name: "Send" }).click();
  await omar.getByText("4 collections set up").waitFor();
  await pick(omar, omar.getByRole("combobox", { name: "Move to column" }), "Review");
  await omar.getByText("Moved to Review").waitFor();
  await tl.getByText(new RegExp(`Card ready for review: ${task}`)).first().waitFor({ timeout: 8000 });
  await omar.keyboard.press("Escape");
});
await step("finishing the client request tells Sales", async () => {
  const c = await cardByTitle(member, request);
  await member.goto(`${projectURL}?card=${c.id}`);
  await pick(member, member.getByRole("combobox", { name: "Move to column" }), "Done");
  await member.getByText("Moved to Done").waitFor();
  await sales.getByText(new RegExp(`Client request done: ${request}`)).first().waitFor({ timeout: 8000 });
  await member.keyboard.press("Escape");
});
await step("Sales see client requests only (no TL tasks); the CEO sees all, with no controls", async () => {
  const mine = (await api(sales, "GET", `/projects/${projectId}/cards`)).json;
  if (!mine.own_only || mine.cards.length !== 1 || mine.cards[0].title !== request) throw new Error(JSON.stringify(mine.cards.map((c) => c.title)));
  await mgmt.goto(projectURL + "?tab=board");
  await mgmt.getByRole("region", { name: "Review" }).getByText(task).waitFor();
  if (await mgmt.getByRole("button", { name: /Add task|Add client request/ }).count()) throw new Error("CEO has an add button");
  await shot(tl, "board");
});

await step("the Board page shows every project's cards in one place", async () => {
  await tl.goto(BASE + "/board");
  await tl.getByRole("region", { name: "Review" }).getByText(task).waitFor();
  await tl.getByRole("region", { name: "Done" }).getByText(request).waitFor();
  const label = await tl.getByRole("button", { name: new RegExp(task) }).innerText();
  if (!label.includes(projectName)) throw new Error("card has no project label: " + label);
  await sales.getByRole("link", { name: "Client requests", exact: true }).click();
  await sales.waitForURL("**/board");
  await sales.getByRole("region", { name: "Done" }).getByText(request).waitFor();
  if (await sales.getByText(task).count()) throw new Error("Sales sees the TL's task");
  await omar.goto(BASE + "/board");
  await omar.getByRole("button", { name: "Only mine" }).click();
  await omar.getByRole("button", { name: new RegExp(task) }).waitFor();
  await omar.waitForTimeout(500);
  if (await omar.getByRole("button", { name: new RegExp(request) }).count()) throw new Error("Only mine shows a card Omar isn't on");
});
await step("Sales find the project by the client's username and log a request", async () => {
  await sales.getByRole("button", { name: "Add client request" }).click();
  // Sales find the project by the client's username (client contact), not the project name.
  await sales.getByRole("textbox", { name: "Client / project" }).fill("ops_" + tag);
  const found = sales.getByRole("dialog").getByRole("option");
  if ((await found.count()) !== 1) throw new Error(`search by client username found ${await found.count()} projects`);
  if (!(await found.innerText()).includes(projectName) || !(await found.innerText()).includes("1,250")) throw new Error("result lacks project name/price: " + (await found.innerText()));
  await sales.getByRole("textbox", { name: "Client / project" }).fill("fo-" + tag.toLowerCase());
  if ((await found.count()) !== 1 || !(await found.innerText()).includes(projectName)) throw new Error("search by order ID failed");
  await found.click();
  await sales.getByRole("textbox", { name: "Request" }).fill(`Size chart ${tag}`);
  await sales.getByRole("dialog").locator("input[type=file]").setInputFiles(path.join(tmp, "client-logo.png"));
  await sales.getByRole("dialog").getByText("client-logo.png").waitFor();
  await sales.getByRole("button", { name: "Add", exact: true }).click();
  await sales.getByRole("region", { name: "To do" }).getByText(`Size chart ${tag}`).waitFor();
  await member.getByText(new RegExp(`New client request on .*Size chart ${tag}`)).first().waitFor({ timeout: 8000 });
});

await step("the client's file rides along and Ops can download it", async () => {
  await member.goto(BASE + "/board");
  await member.getByRole("button", { name: new RegExp(`Size chart ${tag}`) }).click();
  const link = member.getByRole("dialog").getByRole("link", { name: "client-logo.png" });
  await link.waitFor();
  const [dl] = await Promise.all([member.waitForEvent("download"), link.click()]);
  if (fs.readFileSync(await dl.path(), "utf8") !== "logo bytes from the client") throw new Error("downloaded content differs");
  await member.keyboard.press("Escape");
});

// ── 7. The Delivery update moves the project: In Review → Ready to Deliver → Delivered ──
begin("7. Delivery and revisions");
async function deliver(label) {
  await member.goto(projectURL);
  await member.getByRole("button", { name: "New update" }).click();
  await header(member).getByText("UPD-").waitFor();
  await sheet(member).locator("textarea").first().fill(`Delivery ${label} (${tag})`);
  await pick(member, sheet(member).getByRole("combobox").first(), "Delivery · The delivery section (needs an attachment)");
  await member.getByText("Destination set to Delivery").waitFor();
  await sheet(member).getByRole("button", { name: "Submit for approval" }).click();
  await sheet(member).getByText("A Delivery update needs at least one attachment").waitFor();
  await sheet(member).locator("input[type=file]").setInputFiles(path.join(tmp, "delivery.zip"));
  await sheet(member).getByText("delivery.zip").waitFor();
  await sheet(member).getByRole("button", { name: "Submit for approval" }).click();
  await header(member).getByText("Pending Approval").waitFor();
  await member.keyboard.press("Escape");
  if ((await api(member, "GET", `/projects/${projectId}`)).json.project.status !== "ready_for_review") throw new Error("submitting the Delivery didn't send the project for the check");
  await tl.goto(BASE + "/updates?view=approve");
  await tl.locator("tbody tr", { hasText: `Delivery ${label} (${tag})` }).click();
  await sheet(tl).getByRole("button", { name: "Approve" }).click();
  await header(tl).getByText("Approved", { exact: true }).waitFor();
  await sales.getByText(/Ready to deliver on Fiverr/).first().waitFor({ timeout: 8000 });
  await sales.goto(BASE + "/updates?view=send");
  await sales.locator("tbody tr", { hasText: `Delivery ${label} (${tag})` }).click();
  await sheet(sales).getByRole("button", { name: "Mark as sent" }).click();
  await header(sales).getByText("Sent", { exact: true }).waitFor();
}
await step("delivery without attachment is refused; with one it goes through and marks the project delivered", async () => {
  await deliver("v1");
  await sales.goto(projectURL);
  await sales.getByText(/Delivery sent \(update #\d+\)/).waitFor();
  await member.getByText(/was delivered/).first().waitFor({ timeout: 8000 });
});
await step("client revision reopens it; next delivery is Revision 1", async () => {
  await sales.getByRole("button", { name: "Client requested revision" }).click();
  await sheet(sales).locator("textarea").fill("Client wants a darker header.");
  await sheet(sales).getByRole("button", { name: "Client requested revision" }).click();
  await sales.getByText("1 revision").waitFor();
  await deliver("v2");
  await sales.goto(projectURL);
  await sales.getByText(/Revision 1 delivered/).waitFor();
});

// ── 8. Management is read-only ────────────────────────────────────────
begin("8. Management (read-only) and reports");
await step("no actions, no comment box on a project", async () => {
  await mgmt.goto(projectURL);
  await mgmt.waitForLoadState("networkidle");
  // Page content only (not the header bell/sidebar toggle). Opening an update or a
  // file's version list is reading, so those list items don't count as actions.
  const buttons = (await mgmt.locator("main main").getByRole("button").allInnerTexts())
    .map((b) => b.trim())
    .filter((b) => b && !/^\d+ versions$/.test(b) && !b.startsWith("UPD-"));
  const comment = await mgmt.getByPlaceholder("Write a comment…").count();
  if (buttons.length || comment) throw new Error(`buttons: ${buttons.join(", ")} comment: ${comment}`);
});
await step("reports load and the Updates CSV contains this run's updates", async () => {
  await mgmt.goto(BASE + "/reports");
  await mgmt.getByText("Projects by status").waitFor();
  const [dl] = await Promise.all([mgmt.waitForEvent("download"), mgmt.getByRole("link", { name: "Updates CSV" }).click()]);
  const csv = fs.readFileSync(await dl.path(), "utf8");
  if ((csv.match(new RegExp(tag, "g")) ?? []).length < 3) throw new Error("CSV missing rows for " + tag);
});
await step("activity feed shows this project's history", async () => {
  await mgmt.goto(BASE + "/activity");
  await mgmt.getByText(projectName).first().waitFor();
});

// ── 9. Teams, org chart and access ────────────────────────────────────
begin("9. Teams, org chart and access");
await step("member's dashboard shows who they report to; no org chart", async () => {
  await member.goto(BASE + "/");
  await member.getByText("You report to").waitFor();
  const line = await member.getByText("You report to").locator("..").innerText();
  if (!line.includes("Tara Lead")) throw new Error(line);
  await member.goto(BASE + "/hr/org");
  await member.getByText("For Team Leaders, HR and the CEO").waitFor();
});
await step("HR's Teams page lists both teams with their leads and project types", async () => {
  await hr.goto(BASE + "/admin/teams");
  await hr.getByRole("row", { name: /Shopify.*Tara Lead.*Operations.*Shopify Store/ }).waitFor();
  await hr.getByRole("row", { name: /Web Dev.*Ava Admin.*Operations.*Website/ }).waitFor();
});
await step("TL's org chart shows Operations with both teams", async () => {
  await tl.goto(BASE + "/hr/org");
  await tl.getByText("Shopify team").waitFor();
  await tl.getByText("Web Dev team").waitFor();
  await tl.getByRole("tab", { name: "Reporting line" }).click();
  await tl.getByRole("tabpanel").getByText("Milo Member").waitFor();
});
await step("deactivating a Team Leader needs a reason and a handover first", async () => {
  await hr.goto(BASE + "/admin/users");
  await hr.waitForLoadState("networkidle");
  await hr.getByRole("row", { name: /Tara Lead/ }).getByRole("button").last().click();
  await hr.getByRole("menuitem", { name: "Deactivate" }).click();
  const confirm = hr.getByRole("alertdialog").getByRole("button", { name: "Deactivate" });
  if (!(await confirm.isDisabled())) throw new Error("Deactivate enabled without a reason");
  await hr.getByLabel("Reason").fill("QA " + tag);
  await confirm.click();
  await hr.getByText(/Tara Lead still has work to hand over first: .*team.*direct report/).waitFor();
  const me = await api(tl, "GET", "/auth/me");
  if (me.status !== 200) throw new Error("Tara lost access: " + me.status);
});

// ── 10. My work, team workload and the CEO dashboard ──────────────────
begin("10. My work and the dashboards");
await step("a member's My work lists the cards assigned to them, with their project", async () => {
  // Omar holds the TL's task (section 6); it sits in Review, so it's still open work.
  await omar.goto(BASE + "/my-work");
  await omar.waitForLoadState("networkidle");
  await omar.getByRole("heading", { name: "My work" }).waitFor();
  await omar.locator("[data-slot=card-title]", { hasText: "Your cards" }).waitFor();
  await omar.getByText(task).first().waitFor();
  const body = await omar.locator("main main").innerText();
  if (!body.includes(projectName)) throw new Error("card's project is not named");
  // Cards someone else is assigned to don't belong on this page.
  if (body.includes(`Size chart ${tag}`)) throw new Error("shows a card Omar isn't assigned to");
  await shot(omar, "my-work-member", true);
});
await step("Sales' My work shows the requests they logged, not Ops tasks", async () => {
  await sales.goto(BASE + "/my-work");
  await sales.waitForLoadState("networkidle");
  await sales.locator("[data-slot=card-title]", { hasText: "Client requests you logged" }).waitFor();
  const body = await sales.locator("main main").innerText();
  if (!body.includes(`Size chart ${tag}`)) throw new Error("own client request missing");
  if (body.includes(task)) throw new Error("Sales sees the Team Leader's task");
});
await step("the card on My work opens on its project board", async () => {
  await omar.goto(BASE + "/my-work");
  await omar.waitForLoadState("networkidle");
  await omar.getByText(task).first().click();
  await omar.waitForURL(/\/projects\/.*card=/);
  await sheet(omar).getByText(task).first().waitFor();
  await omar.keyboard.press("Escape");
});
await step("the Team Leader's dashboard shows their team's workload", async () => {
  await tl.goto(BASE + "/");
  await tl.waitForLoadState("networkidle");
  await tl.locator("[data-slot=card-title]", { hasText: "Shopify team workload" }).waitFor();
  const row = tl.getByRole("row", { name: /Milo Member/ });
  await row.waitFor();
  await tl.getByRole("row", { name: /Omar Member/ }).waitFor();
  if (!(await row.innerText()).match(/\d/)) throw new Error("no numbers in Milo's row");
  // Mina is on the Web Dev team, so she must not appear here.
  if (await tl.getByRole("row", { name: /Mina Member/ }).count()) throw new Error("another team's member is listed");
  await tl.locator("[data-slot=card-title]", { hasText: "Who" }).first().waitFor();
  await shot(tl, "tl-dashboard", true);
});
await step("the CEO's dashboard shows company numbers and every team", async () => {
  await mgmt.goto(BASE + "/");
  await mgmt.waitForLoadState("networkidle");
  await mgmt.getByText(/Received for projects completed in/).waitFor();
  await mgmt.getByText("People in today").waitFor();
  // Every team, with its lead, and both teams' members in one workload table.
  await mgmt.getByRole("row", { name: /Shopify.*Tara Lead/ }).waitFor();
  await mgmt.getByRole("row", { name: /Web Dev.*Ava Admin/ }).waitFor();
  await mgmt.locator("[data-slot=card-title]", { hasText: "Workload across the company" }).waitFor();
  await mgmt.getByRole("row", { name: /Mina Member.*Web Dev/ }).waitFor();
  // The CEO's own counts row is replaced by this overview.
  if (await mgmt.getByText("Active projects").count()) throw new Error("the generic stat row is still there");
  await shot(mgmt, "ceo-dashboard", true);
});

await step("the admin sees the Google Sheet backup, switched off and queueing", async () => {
  await admin.goto(BASE + "/admin/settings");
  await admin.waitForLoadState("networkidle");
  await admin.locator("[data-slot=card-title]", { hasText: "Google Sheet backup" }).waitFor();
  await admin.getByText("Not switched on yet.").waitFor();
  // The queue fills anyway, so configuring a sheet later backs up what already happened.
  const status = (await api(admin, "GET", "/admin/sheet")).json;
  if (status.configured !== false || !(status.total > 0) || status.total !== status.pending) {
    throw new Error("status: " + JSON.stringify(status));
  }
  await shot(admin, "sheet-backup");
});

// ── 11. Chat and voice calls ──────────────────────────────────────────
begin("11. Chat and voice calls");
await step("a message arrives without a reload and the unread count shows", async () => {
  await tl.goto(BASE + "/messages");
  await tl.waitForLoadState("networkidle");
  // Start a conversation by finding the person.
  await tl.getByRole("textbox", { name: "Find a person" }).fill("Milo");
  await tl.getByRole("button", { name: /Milo Member/ }).first().click();
  await tl.getByRole("textbox", { name: "Message", exact: true }).fill(`Can you take the Acme store? (${tag})`);
  await tl.getByRole("button", { name: "Send" }).click();
  // The same words also appear in the inbox preview, so look inside the conversation.
  await tl.getByText(`Can you take the Acme store? (${tag})`).first().waitFor();

  // Milo's sidebar shows it waiting, pushed over the live stream.
  await member.goto(BASE + "/");
  await member.waitForLoadState("networkidle");
  await member.getByRole("link", { name: /Messages \(\d+\)/ }).waitFor({ timeout: 10000 });
  await member.getByRole("link", { name: /Messages \(\d+\)/ }).click();
  await member.waitForURL("**/messages");
  await member.getByRole("button", { name: /Tara Lead/ }).first().click();
  await member.getByText(`Can you take the Acme store? (${tag})`).first().waitFor();
  // Reading it clears the count.
  await member.getByRole("link", { name: "Messages", exact: true }).waitFor({ timeout: 10000 });

  // The reply reaches the Team Leader's open page with no reload.
  await member.getByRole("textbox", { name: "Message", exact: true }).fill(`Starting now (${tag})`);
  await member.keyboard.press("Enter");
  await tl.getByText(`Starting now (${tag})`).first().waitFor({ timeout: 10000 });
  await shot(tl, "messages");
});

await step("nobody else can read that conversation", async () => {
  const theirs = await api(sales, "GET", `/messages/${e2eIds.milo}`);
  if (theirs.status !== 200 || theirs.json.messages.length !== 0) {
    throw new Error("Sales can read the TL's thread with Milo: " + JSON.stringify(theirs.json).slice(0, 120));
  }
});

await step("a voice call rings, is answered, and both ends talk", async () => {
  await tl.goto(BASE + "/messages?with=" + e2eIds.milo);
  await tl.waitForLoadState("networkidle");
  await tl.getByRole("button", { name: "Call" }).click();
  // Milo is rung wherever he is in the app.
  const incoming = member.getByRole("dialog", { name: /Tara Lead is calling you/ });
  await incoming.waitFor({ timeout: 15000 });
  await incoming.getByRole("button", { name: "Answer" }).click();
  // Both sides reach a real peer connection: the timer only starts when audio is up.
  await tl.getByRole("dialog", { name: /On a call with Milo Member/ }).getByText(/^\d+:\d\d$/).waitFor({ timeout: 20000 });
  await member.getByRole("dialog").getByText(/^\d+:\d\d$/).waitFor({ timeout: 20000 });
  // Mute is a real track change, not just a label.
  await member.getByRole("button", { name: "Mute" }).click();
  await member.getByRole("button", { name: "Unmute" }).waitFor();
  await shot(tl, "call");
  // Hanging up clears it at both ends.
  await tl.getByRole("button", { name: "Hang up" }).click();
  await tl.getByRole("dialog", { name: /On a call/ }).waitFor({ state: "detached", timeout: 10000 });
  await member.getByRole("dialog").waitFor({ state: "detached", timeout: 10000 });
  const log = (await api(tl, "GET", "/calls")).json.calls;
  if (!log.length || log[0].status !== "ended" || log[0].answered_at === null) {
    throw new Error("the call log is wrong: " + JSON.stringify(log[0] ?? null));
  }
});

await step("a declined call says so, and only the person called may answer", async () => {
  await tl.getByRole("button", { name: "Call" }).click();
  const incoming = member.getByRole("dialog", { name: /is calling you/ });
  await incoming.waitFor({ timeout: 15000 });
  const live = (await api(tl, "GET", "/calls/live")).json.call;
  // The caller cannot answer their own call.
  const cheat = await api(tl, "POST", `/calls/${live.id}/status`, { status: "answered" });
  if (cheat.status !== 403) throw new Error("the caller answered their own call: " + cheat.status);
  await incoming.getByRole("button", { name: "Decline" }).click();
  await tl.getByText(/Milo Member declined/).waitFor({ timeout: 10000 });
  await tl.getByRole("dialog", { name: /On a call/ }).waitFor({ state: "detached", timeout: 10000 });
});

await step("everyone may message and ring anyone, the CEO included", async () => {
  await mgmt.goto(BASE + "/messages");
  await mgmt.waitForLoadState("networkidle");
  await mgmt.getByRole("textbox", { name: "Find a person" }).fill("Milo");
  await mgmt.getByRole("button", { name: /Milo Member/ }).first().click();
  await mgmt.getByRole("textbox", { name: "Message", exact: true }).fill(`Good work this week (${tag})`);
  await mgmt.getByRole("button", { name: "Send" }).click();
  await mgmt.getByText(`Good work this week (${tag})`).first().waitFor({ timeout: 10000 });
  // Talking is not operating a project, so the read-only CEO may ring too.
  const rung = await api(mgmt, "POST", `/calls/${e2eIds.milo}`);
  if (rung.status !== 201) throw new Error("the CEO cannot ring anyone: " + rung.status);
  await api(mgmt, "POST", `/calls/${rung.json.call.id}/status`, { status: "ended" });
});

let groupId;
await step("a group reaches everyone in it and nobody outside it", async () => {
  await tl.goto(BASE + "/messages");
  await tl.waitForLoadState("networkidle");
  await tl.getByRole("button", { name: "New group" }).click();
  const form = tl.getByRole("dialog");
  await form.getByLabel("Name").fill("Ops " + tag);
  await form.getByLabel("People").fill("Milo");
  await form.getByRole("button", { name: /Milo Member/ }).click();
  await form.getByRole("button", { name: "Create group" }).click();
  await tl.waitForURL("**/messages?group=*");
  groupId = new URL(tl.url()).searchParams.get("group");
  await tl.getByRole("textbox", { name: "Message", exact: true }).fill(`Stand-up at 10 (${tag})`);
  await tl.getByRole("button", { name: "Send" }).click();
  await tl.getByText(`Stand-up at 10 (${tag})`).first().waitFor();

  // Milo is in it, so the group is in his list with the message waiting.
  await member.goto(BASE + "/messages");
  await member.waitForLoadState("networkidle");
  await member.getByRole("button", { name: new RegExp("Ops " + tag) }).first().click();
  await member.getByText(`Stand-up at 10 (${tag})`).first().waitFor({ timeout: 10000 });
  // He can answer, and the Team Leader's open page shows it with no reload.
  await member.getByRole("textbox", { name: "Message", exact: true }).fill(`See you there (${tag})`);
  await member.keyboard.press("Enter");
  await tl.getByText(`See you there (${tag})`).first().waitFor({ timeout: 10000 });
  await shot(tl, "group");

  // Sales were never added, so for them it does not exist.
  const peek = await api(sales, "GET", `/groups/${groupId}`);
  if (peek.status !== 404) throw new Error("an outsider can read the group: " + peek.status);
  if ((await api(sales, "GET", "/groups")).json.groups.some((g) => g.id === groupId)) {
    throw new Error("an outsider is listed in the group");
  }
});

await step("the owner adds and removes; anyone may leave", async () => {
  // Milo (a member, not the owner) brings Sales in; Sales then see the history.
  await api(member, "POST", `/groups/${groupId}/members`, { user_ids: [e2eIds.sam] });
  const joined = await api(sales, "GET", `/groups/${groupId}`);
  if (joined.status !== 200 || joined.json.messages.length < 2) {
    throw new Error("Sales joined to an empty room: " + JSON.stringify(joined.json).slice(0, 120));
  }
  // Renaming is the owner's, and a member cannot throw anyone else out.
  const rename = await api(member, "PATCH", `/groups/${groupId}`, { name: "Not mine" });
  if (rename.status !== 403) throw new Error("a member renamed the group: " + rename.status);
  const evict = await api(sales, "DELETE", `/groups/${groupId}/members/${e2eIds.milo}`);
  if (evict.status !== 403) throw new Error("a member removed someone else: " + evict.status);
  // But anyone may walk out.
  const left = await api(sales, "DELETE", `/groups/${groupId}/members/${e2eIds.sam}`);
  if (left.status !== 204) throw new Error("Sales could not leave: " + left.status);
  if ((await api(sales, "GET", `/groups/${groupId}`)).status !== 404) throw new Error("the group is still readable after leaving");
});

// ── 12. Expense invoices (Office Admin → HR → CEO) ────────────────────
begin("12. Expense invoices");
{
  const office = await session("office");
  const nav = await navOf(office);
  check(
    "the Office Admin sees invoices and nothing about projects",
    nav.includes("Invoices") && nav.includes("My HR") && !nav.includes("Projects") && !nav.includes("Finance") && !nav.includes("Payroll"),
    nav.join(", "),
  );

  // One purchase, three lines, two categories: 2,000 + 4 × 1,000 + 500 = Tk 6,500.
  const what = `Meeting room kit (${tag})`;
  // The tag rides on a line too, so this run's ledger entry is telling apart from the last run's.
  const fan = `Ceiling fan (${tag})`;
  await step("the Office Admin raises an invoice with several lines", async () => {
    await office.goto(BASE + "/invoices");
    await office.getByRole("button", { name: "Raise invoice" }).click();
    const d = sheet(office);
    await d.getByLabel("What is it for").fill(what);
    const line = async (i, item, category, qty, price) => {
      if (i > 1) await d.getByRole("button", { name: "Add a line" }).click();
      await d.getByLabel(`Item ${i}`, { exact: true }).fill(item);
      await pick(office, d.getByLabel(`Category for item ${i}`), category);
      await d.getByLabel(`Quantity for item ${i}`).fill(qty);
      await d.getByLabel(`Unit price for item ${i}`).fill(price);
    };
    await line(1, fan, "Equipment", "1", "2000");
    await line(2, "Office chair", "Equipment", "4", "1000");
    await line(3, "Tea and sugar", "Food & refreshments", "1", "500");
    await d.getByText("Total Tk 6,500").waitFor();
    await d.getByRole("button", { name: "Raise invoice" }).click();
    await office.getByText("Waiting for HR").first().waitFor();
  });
  const raised = (await api(office, "GET", "/finance/invoices")).json.invoices.find((i) => i.description === what);
  check(
    "the lines add up and nothing is in the ledger yet",
    raised?.status === "pending" && raised?.amount_bdt === "6500.00" && raised?.items === 3 && raised?.ledger_lines === 0,
    `${raised?.status} ${raised?.amount_bdt} over ${raised?.items}`,
  );

  await step("HR approves it and it goes to the CEO", async () => {
    await hr.goto(BASE + "/invoices");
    await invoiceCard(hr, what).getByRole("button", { name: "Approve" }).click();
    await sheet(hr).getByLabel("Note (optional)").fill(`Needed (${tag})`);
    await sheet(hr).getByRole("button", { name: "Approve" }).click();
    await hr.getByText("Approved, sent to the CEO").waitFor({ timeout: 10000 });
  });
  const afterHR = (await api(hr, "GET", "/finance/invoices")).json.invoices.find((i) => i.description === what);
  check("HR's approval posts nothing to the ledger", afterHR?.status === "awaiting_ceo" && afterHR?.ledger_lines === 0, afterHR?.status);
  check("HR cannot give the final approval", (await api(hr, "POST", `/finance/invoices/${raised.id}/approve`, { note: "" })).status === 403);

  await step("the CEO gives the final approval", async () => {
    await mgmt.goto(BASE + "/invoices");
    await invoiceCard(mgmt, what).getByRole("button", { name: "Approve" }).click();
    await sheet(mgmt).getByRole("button", { name: "Approve" }).click();
    await mgmt.getByText(/approved and added to the ledger/).waitFor({ timeout: 10000 });
  });
  const approved = (await api(mgmt, "GET", "/finance/invoices")).json.invoices.find((i) => i.description === what);
  check("the approved invoice posts one ledger line per category", approved?.status === "approved" && approved?.ledger_lines === 2, `${approved?.status}, ${approved?.ledger_lines} lines`);

  const posted = {};
  for (const e of (await api(hr, "GET", `/finance/entries?from=${approved.expense_date}&to=${approved.expense_date}&kind=expense`)).json.entries) {
    if (e.invoice_id === approved.id) posted[e.category_name] = e;
  }
  check(
    "each category gets its own amount, named by the lines it covers",
    posted["Equipment"]?.amount_bdt === "6000.00" &&
      posted["Equipment"]?.description === `${fan}, Office chair` &&
      posted["Food & refreshments"]?.amount_bdt === "500.00",
    Object.entries(posted).map(([k, v]) => `${k} ${v.amount_bdt}`).join(", "),
  );
  check("the raiser is credited on the ledger line", posted["Equipment"]?.created_by_name === "Omi Office", posted["Equipment"]?.created_by_name);
  check(
    "HR cannot delete an expense the CEO approved",
    posted["Equipment"] && (await api(hr, "DELETE", `/finance/entries/${posted["Equipment"].id}`)).status === 409,
  );

  await step("the ledger names the invoice and offers HR no edit", async () => {
    await hr.goto(BASE + "/finance");
    await hr.getByRole("tab", { name: "Ledger" }).click();
    const row = hr.getByRole("row").filter({ hasText: `${fan}, Office chair` });
    await row.getByRole("link", { name: /STV-\d+/ }).waitFor();
    if (await row.getByRole("button", { name: "Delete entry" }).count()) throw new Error("HR was offered Delete on an approved expense");
  });

  // The printed voucher: the company's own debit-voucher form, built from the same invoice.
  await step("the voucher prints as the company's debit voucher", async () => {
    await office.goto(`${BASE}/invoices/${approved.id}`);
    const doc = office.locator("article.print-document");
    await doc.getByText("DEBIT VOUCHER").waitFor();
    for (const text of ["Growvence Tech", `No.: STV-`, what, fan, "In Words: Six Thousand Five Hundred Taka only",
                        "TOTAL: 6,500", "Received By", "Verified By HR & Compliance", "Approved By CEO"]) {
      await doc.getByText(text, { exact: false }).first().waitFor();
    }
    // Printing drops the app's own chrome and leaves only the voucher.
    await office.emulateMedia({ media: "print" });
    const chrome = await office.evaluate(() =>
      [...document.querySelectorAll("button, a")].filter((el) => el.offsetParent !== null).length,
    );
    const docVisible = await doc.isVisible();
    await office.emulateMedia({ media: null });
    if (chrome !== 0 || !docVisible) throw new Error(`print still shows ${chrome} controls, voucher visible: ${docVisible}`);
  });
  await shot(office, "voucher");

  check("the Office Admin cannot approve their own", (await api(office, "POST", `/finance/invoices/${raised.id}/approve`, {})).status === 403);
  check("the Office Admin cannot read the ledger", (await api(office, "GET", "/finance/entries")).status === 403);
  check("the Office Admin cannot open payroll", (await api(office, "GET", "/finance/payroll")).status === 403);
  check("expenses cannot be entered straight into the ledger", (await api(hr, "POST", "/finance/entries", {
    category_id: posted["Equipment"].category_id, amount: "100", entry_date: approved.expense_date, description: `By hand (${tag})`,
  })).status === 422);
}

// ── 13. Client requests with no project ───────────────────────────────
begin("13. Client requests (no project)");
{
  const client = `Nadia ${tag}`;
  const what = `Add a size chart (${tag})`;
  let requestId;
  await step("Sales raise one for a client who has no project here", async () => {
    await sales.goto(BASE + "/requests");
    await sales.getByRole("button", { name: "New client request" }).click();
    const d = sheet(sales);
    await pick(sales, d.getByLabel("Profile the update goes out on"), "Studio Main · Fiverr");
    await d.getByLabel("Client", { exact: true }).fill(client);
    await d.getByLabel("What the client asked for").fill(what);
    await d.getByLabel("Details").fill("Monthly management client; no order left to hang it on.");
    await pick(sales, d.getByLabel("Team to do it"), "Shopify · Tara Lead");
    await d.getByRole("button", { name: "Send to the team" }).click();
    await sales.getByText(client).first().waitFor();
    requestId = (await api(sales, "GET", "/client-requests")).json.requests.find((r) => r.title === what)?.id;
    if (!requestId) throw new Error("the request is not in the list");
  });
  const requestURL = () => `${BASE}/requests/${requestId}`;

  await step("it is not a project, and the order ID stayed empty", async () => {
    const r = (await api(sales, "GET", `/client-requests/${requestId}`)).json.request;
    if (r.order_id !== null || r.status !== "open" || r.team_name !== "Shopify") throw new Error(JSON.stringify(r));
    const projects = (await api(sales, "GET", "/projects?q=" + encodeURIComponent(client))).json;
    if (projects.total !== 0) throw new Error("it turned up in the project list");
  });

  await step("the team's board carries its card, labelled by the client", async () => {
    await tl.goto(BASE + "/board");
    await tl.waitForLoadState("networkidle");
    await tl.getByText(what).first().waitFor();
    const cards = (await api(tl, "GET", "/cards")).json.cards;
    if (!cards.some((c) => c.client_request_id === requestId)) throw new Error("not on the board");
  });

  await step("everyone on the team sees it before the work is handed out", async () => {
    await member.goto(requestURL());
    await member.getByText(what).first().waitFor();
    const r = await api(member, "GET", `/client-requests/${requestId}`);
    if (r.status !== 200) throw new Error("the team's member cannot open it: " + r.status);
    if (r.json.request.member_names.length) throw new Error("someone already holds it");
    // Reading it is not working it: the updates are still the card holder's to write.
    if (r.json.permissions.can_write_note) throw new Error("a teammate holding no card can write updates");
    // The board is the team's, though, so they can drag its cards along.
    if (!r.json.can_move_card) throw new Error("a teammate cannot move the cards");
    if (r.json.permissions.can_assign) throw new Error("a teammate can hand the work out");
    const card = r.json.cards[0].id;
    const moved = await api(member, "POST", `/cards/${card}/move`, { column: "in_progress", index: 0 });
    if (moved.status !== 200) throw new Error("the move was refused: " + moved.status);
    const back = await api(member, "POST", `/cards/${card}/move`, { column: "todo", index: 0 });
    if (back.status !== 200) throw new Error("moving it back was refused: " + back.status);
  });

  await step("the Team Leader gives the work out on the request itself", async () => {
    await tl.goto(requestURL());
    // Whoever holds the work writes the updates, so it is given out here, not only in the card.
    await tl.getByText("Nobody yet — pick someone").waitFor();
    await tl.getByRole("button", { name: "Milo Member", pressed: false }).click();
    await tl.getByRole("button", { name: "Milo Member", pressed: true }).waitFor();
    const r = (await api(tl, "GET", `/client-requests/${requestId}`)).json;
    if (!r.request.member_names.includes("Milo Member")) throw new Error("not recorded: " + JSON.stringify(r.request.member_names));
    // The team's leader is on the list too, so a one-person team can always take its own work.
    if (!r.roster.some((m) => m.name === "Tara Lead")) throw new Error("the leader is missing from the list");
  });

  let updateId;
  await step("the member writes the client update on the request", async () => {
    await member.goto(requestURL());
    await member.getByRole("button", { name: "New update" }).click();
    await header(member).getByText("UPD-").waitFor();
    await sheet(member).locator("textarea").first().fill(`Size chart is live on every product page (${tag}).`);
    await sheet(member).getByRole("button", { name: "Submit for approval" }).click();
    await header(member).getByText("Pending Approval").waitFor();
    updateId = new URL(member.url()).searchParams.get("open") ??
      (await api(member, "GET", `/updates?client_request_id=${requestId}`)).json.updates[0].id;
    await member.keyboard.press("Escape");
  });

  await step("it carries the client, the order ID and the profile from the request", async () => {
    const u = (await api(tl, "GET", `/updates/${updateId}`)).json.update;
    if (u.project_id !== null || u.client_request_id !== requestId || u.client_name !== client || u.profile_name !== "Studio Main") {
      throw new Error(JSON.stringify({ project: u.project_id, request: u.client_request_id, client: u.client_name, profile: u.profile_name }));
    }
  });

  await step("the Team Leader approves it and Sales post it", async () => {
    await tl.goto(`${BASE}/updates?view=all&open=${updateId}`);
    await sheet(tl).getByRole("button", { name: "Approve" }).click();
    await header(tl).getByText("Approved", { exact: true }).waitFor();
    await sales.goto(`${BASE}/updates?view=send&open=${updateId}`);
    await sheet(sales).getByRole("button", { name: "Mark as sent" }).click();
    await header(sales).getByText("Sent", { exact: true }).waitFor();
  });

  await step("Sales close the request once the client has it", async () => {
    await sales.goto(requestURL());
    await sales.getByRole("button", { name: "Mark done" }).click();
    await sales.getByText("Done").first().waitFor();
    const r = (await api(sales, "GET", `/client-requests/${requestId}`)).json.request;
    if (r.status !== "done") throw new Error("status " + r.status);
  });

  await step("a request on a team with nobody on it still opens", async () => {
    const team = (await api(mgmt, "POST", "/admin/teams", { name: `Nobody ${tag}` })).json.team;
    const profile = (await api(sales, "GET", "/profiles")).json.profiles[0];
    const raised = (await api(sales, "POST", "/client-requests", {
      profile_id: profile.id, client_name: `Lonely ${tag}`, title: `No team members (${tag})`, details: "", team_id: team.id,
    })).json.request;
    await sales.goto(`${BASE}/requests/${raised.id}`);
    await sales.getByText(`No team members (${tag})`).first().waitFor();
    const d = (await api(sales, "GET", `/client-requests/${raised.id}`)).json;
    // A null here blanked the page once: every list has to be a list.
    if (!Array.isArray(d.roster) || !Array.isArray(d.cards) || !Array.isArray(d.activity)) {
      throw new Error("a list came back as null: " + JSON.stringify({ roster: d.roster, cards: d.cards, activity: d.activity }));
    }
  });

  const probes = [
    ["a request has no delivery to send", member, "POST", `/client-requests/${requestId}/updates`, { destination: "delivery" }, 422],
    ["Sales never write the updates", sales, "POST", `/client-requests/${requestId}/updates`, { destination: "inbox" }, 403],
    ["the Team Leader does not raise requests", tl, "POST", "/client-requests", { client_name: "x", title: "x" }, 403],
    ["the Team Leader does not close them", tl, "POST", `/client-requests/${requestId}/status`, { status: "done" }, 403],
    ["another team's leader cannot see it", admin, "GET", `/client-requests/${requestId}`, null, 404],
    ["HR cannot see it", hr, "GET", `/client-requests/${requestId}`, null, 404],
    ["the CEO reads it and changes nothing", mgmt, "POST", `/client-requests/${requestId}/status`, { status: "open" }, 403],
  ];
  for (const [name, page, method, p, body, want] of probes) {
    const r = await api(page, method, p, body);
    check(name, r.status === want, `got ${r.status}`);
  }
}

// ── 14. Permission probes (server-side enforcement) ───────────────────
begin("14. Server-side permission probes");
const requestCard = await cardByTitle(tl, request);
const probes = [
  ["Management cannot change status", mgmt, "POST", `/projects/${projectId}/transition`, { to: "cancelled" }, 403],
  ["Management cannot comment", mgmt, "POST", `/projects/${projectId}/comments`, { body: "x" }, 403],
  ["Sales cannot assign", sales, "POST", `/projects/${projectId}/assign`, { assignee_id: projectId, priority: "low" }, 403],
  ["Member cannot approve updates", member, "POST", `/updates/${updateId}/approve`, {}, 403],
  ["TL cannot open Management reports", tl, "GET", "/reports/summary", null, 403],
  ["TL cannot export CSV", tl, "GET", "/reports/export/updates.csv", null, 403],
  ["Sales cannot read the activity log", sales, "GET", "/activity", null, 403],
  ["Non-admin cannot list users", tl, "GET", "/admin/users", null, 403],
  ["Admin flag alone cannot list users", admin, "GET", "/admin/users", null, 403],
  ["HR cannot see projects", hr, "GET", "/reports/summary", null, 403],
  ["Member 2 cannot see Milo's project", null, "GET", `/projects/${projectId}`, null, 404],
  ["Another team's lead cannot see the project", admin, "GET", `/projects/${projectId}`, null, 404],
  ["Another team's lead cannot comment on it", admin, "POST", `/projects/${projectId}/comments`, { body: "x" }, 404],
  ["TL cannot create teams", tl, "POST", "/admin/teams", { name: "QA " + tag }, 403],
  ["Admin flag alone cannot create teams", admin, "POST", "/admin/teams", { name: "QA " + tag }, 403],
  ["HR cannot route project types", hr, "PATCH", `/admin/project-types/${projectId}`, { team_id: null }, 403],
  ["Members cannot create cards", member, "POST", `/projects/${projectId}/cards`, { title: "x" }, 403],
  ["Sales cannot move cards", sales, "POST", `/cards/${requestCard.id}/move`, { column: "todo" }, 403],
  ["CEO cannot move cards", mgmt, "POST", `/cards/${requestCard.id}/move`, { column: "todo" }, 403],
  ["CEO cannot add cards", mgmt, "POST", `/projects/${projectId}/cards`, { title: "x" }, 403],
  ["Another team's lead cannot see the board", admin, "GET", `/projects/${projectId}/cards`, null, 404],
  ["Member 2 cannot see the board", null, "GET", `/projects/${projectId}/cards`, null, 404],
  ["HR sees no cards", hr, "GET", "/cards", null, 200],
  ["Sales cannot see the workload", sales, "GET", "/reports/workload", null, 403],
  ["A member cannot see the workload", member, "GET", "/reports/workload", null, 403],
  ["HR cannot see the workload", hr, "GET", "/reports/workload", null, 403],
  ["TL cannot open the company dashboard", tl, "GET", "/reports/company", null, 403],
  ["TL cannot read the sheet backup status", tl, "GET", "/admin/sheet", null, 403],
  ["HR cannot trigger a sheet backup", hr, "POST", "/admin/sheet/backfill", null, 403],
  ["Sales cannot test the sheet connection", sales, "POST", "/admin/sheet/check", null, 403],
];
const member2 = await session("member2");
for (const [name, page, method, p, body, want] of probes) {
  const r = await api(page ?? member2, method, p, body);
  check(name, r.status === want, `got ${r.status}`);
}

// ── 15. Layout at phone width ─────────────────────────────────────────
begin("15. Phone layout (390px)");
const phone = await session("tl", { width: 390, height: 844 });
for (const p of ["/", "/my-work", "/projects", "/updates", "/requests", `/projects/${projectId}`, `/projects/${projectId}?tab=board`, "/board", "/activity", "/hr/org"]) {
  await phone.goto(BASE + p);
  await phone.waitForLoadState("networkidle");
  const overflow = await phone.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  check(`${p} fits the screen`, overflow <= 1, `overflow ${overflow}px`);
}
await shot(phone, "phone-project");
{
  const phoneOffice = await session("office", { width: 390, height: 844 });
  await phoneOffice.goto(BASE + "/invoices");
  await phoneOffice.waitForLoadState("networkidle");
  const overflow = await phoneOffice.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  check("/invoices fits the screen", overflow <= 1, `overflow ${overflow}px`);
  await shot(phoneOffice, "phone-invoices");
}

// ── Summary ───────────────────────────────────────────────────────────
begin("Console");
check("no console errors in any session", consoleErrors.length === 0, consoleErrors.slice(0, 3).join(" | "));
await browser.close();
fs.rmSync(tmp, { recursive: true, force: true });

const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} checks passed against ${BASE} (run ${tag})`);
if (failed.length) {
  console.log("Failures:");
  for (const f of failed) console.log(`  - [${f.section}] ${f.name}: ${f.detail}`);
}
fs.writeFileSync(process.env.QA_REPORT ?? "qa-report.json", JSON.stringify({ base: BASE, tag, results, consoleErrors }, null, 2));
process.exit(failed.length ? 1 : 0);
