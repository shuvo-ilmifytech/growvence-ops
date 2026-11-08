// Package httpapi is the Fiber HTTP layer: routing, middleware and handlers.
package httpapi

import (
	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
	"log/slog"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/chat"
	"github.com/solvinx/opsflow/internal/config"
	"github.com/solvinx/opsflow/internal/finance"
	"github.com/solvinx/opsflow/internal/hr"
	"github.com/solvinx/opsflow/internal/live"
	"github.com/solvinx/opsflow/internal/mcp"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/reports"
	"github.com/solvinx/opsflow/internal/sheets"
	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
)

const sessionCookie = "opsflow_session"

type Server struct {
	cfg      config.Config
	store    *store.Store
	rdb      *redis.Client
	sessions *auth.SessionStore
	projects *projects.Service
	files    *storage.Storage
	hub      *notify.Hub
	reports  *reports.Service
	hr       *hr.Service
	finance  *finance.Service
	// sheets is nil unless a Google Sheet backup is configured.
	sheets *sheets.Client
	backup *sheets.Syncer
	chat   *chat.Service
	live   *live.Bus
	// mcpServer answers AI assistants on /mcp (Phase 14).
	mcpServer *mcp.Server
}

func New(cfg config.Config, st *store.Store, rdb *redis.Client, files *storage.Storage, hub *notify.Hub) *fiber.App {
	app, _, _, _ := NewWithServices(cfg, st, rdb, files, hub)
	return app
}

// NewWithServices also returns the services that need scheduling: the projects service
// (scheduled checks), the Google Sheet backup (nil unless one is configured) and chat
// (unanswered calls time out).
func NewWithServices(cfg config.Config, st *store.Store, rdb *redis.Client, files *storage.Storage, hub *notify.Hub) (*fiber.App, *projects.Service, *sheets.Syncer, *chat.Service) {
	s := &Server{
		cfg:      cfg,
		store:    st,
		rdb:      rdb,
		sessions: auth.NewSessionStore(rdb, cfg.SessionTTL, cfg.SessionMaxAge),
		projects: projects.NewService(st, files, rdb, cfg.MaxUploadSize),
		files:    files,
		hub:      hub,
		reports:  reports.NewService(st),
		hr:       hr.NewService(st),
	}
	s.finance = finance.NewService(st, s.hr)
	s.live = live.NewBus(rdb)
	s.chat = chat.NewService(st, s.live, files, rdb, cfg.MaxUploadSize)
	if cfg.SheetsEnabled {
		c, err := sheets.New(cfg.Sheets)
		if err != nil {
			// Bad credentials shouldn't stop the app: the queue waits, the admin page says why.
			slog.Error("google sheet backup is configured but unusable", "err", err)
		} else {
			s.sheets, s.backup = c, sheets.NewSyncer(st, c)
		}
	}

	app := fiber.New(fiber.Config{
		AppName:      "opsflow-api",
		ErrorHandler: errorHandler,
		BodyLimit:    1 << 20, // JSON only; files go straight to S3 via presigned URLs
		// The API is reached only through Caddy (and Next.js) on the private Docker
		// network. Caddy replaces X-Forwarded-For with the address that connected to it,
		// so c.IP() is the client's real public IP; a request from anywhere else keeps
		// its own address and can't spoof the header.
		TrustProxy:         true,
		TrustProxyConfig:   fiber.TrustProxyConfig{Private: true, Loopback: true},
		ProxyHeader:        fiber.HeaderXForwardedFor,
		EnableIPValidation: true,
	})

	app.Get("/healthz", s.health)
	s.mountMCP(app)

	api := app.Group("/api", s.originGuard)

	api.Post("/auth/login", s.login)
	api.Post("/auth/logout", s.logout)

	authed := api.Group("", s.requireAuth, readOnlyGuard)
	authed.Get("/auth/me", s.me)
	authed.Post("/auth/password", s.changePassword)

	authed.Get("/search", s.search)
	authed.Get("/users/directory", s.userDirectory)
	authed.Get("/profiles", s.listProfiles)
	authed.Get("/project-types", s.listProjectTypes)
	authed.Get("/teams", s.listTeams)
	authed.Get("/settings", s.listSettings)

	// Projects: role scoping and every workflow rule live in the projects service.
	authed.Get("/projects", s.listProjects)
	authed.Post("/projects", s.createProject)
	authed.Get("/projects/:id", s.getProject)
	authed.Patch("/projects/:id", s.updateProject)
	authed.Post("/projects/:id/transition", s.transitionProject)
	authed.Post("/projects/:id/assign", s.assignProject)
	authed.Post("/projects/:id/team", s.moveProjectTeam)
	authed.Put("/projects/:id/planning", s.planProject)
	authed.Post("/projects/:id/comments", s.commentProject)
	authed.Put("/projects/:id/store-access", s.setStoreAccess)
	authed.Post("/projects/:id/acknowledge", s.acknowledgeProject)
	authed.Post("/projects/:id/view", s.viewProject)
	authed.Post("/projects/:id/files/uploads", s.initUpload)
	authed.Post("/projects/:id/files/uploads/:upload_id/complete", s.completeUpload)
	authed.Delete("/projects/:id/files/:file_id", s.removeFile)
	authed.Get("/files/versions/:version_id/download", s.downloadFile)

	// Client requests: work for a client with no project in the portal. Sales raise them
	// and send the approved updates; the team works their cards and writes the updates.
	authed.Get("/client-requests", s.listRequests)
	authed.Post("/client-requests", s.raiseRequest)
	authed.Get("/client-requests/:id", s.getRequest)
	authed.Patch("/client-requests/:id", s.updateRequest)
	authed.Post("/client-requests/:id/team", s.moveRequestTeam)
	authed.Post("/client-requests/:id/status", s.setRequestStatus)
	authed.Post("/client-requests/:id/cards", s.createRequestCard)
	authed.Post("/client-requests/:id/updates", s.createRequestUpdate)

	// Project board: client requests (Sales) and tasks (Team Leader) in four columns.
	authed.Get("/projects/:id/cards", s.projectBoard)
	authed.Post("/projects/:id/cards", s.createCard)
	authed.Get("/cards", s.allCards) // every card across the user's projects
	authed.Get("/cards/:cid", s.getCard)
	authed.Patch("/cards/:cid", s.updateCard)
	authed.Post("/cards/:cid/move", s.moveCard)
	authed.Put("/cards/:cid/assignees", s.setCardAssignees)
	authed.Post("/cards/:cid/comments", s.commentCard)
	authed.Delete("/cards/:cid", s.removeCard)
	authed.Post("/cards/:cid/attachments/uploads", s.initCardUpload)
	authed.Post("/cards/:cid/attachments/uploads/:upload_id/complete", s.completeCardUpload)
	authed.Delete("/cards/:cid/attachments/:file_id", s.removeCardAttachment)
	authed.Post("/cards/:cid/opened", s.markCardOpened)
	authed.Get("/alerts", s.alerts) // what should be ringing right now

	// Chat and voice calls between two people.
	authed.Post("/chat/uploads", s.initChatUpload)
	authed.Get("/chat/attachments/:aid", s.chatAttachment)
	authed.Get("/account/assistant-tokens", s.listMCPTokens)
	authed.Post("/account/assistant-tokens", s.createMCPToken)
	authed.Delete("/account/assistant-tokens/:tid", s.revokeMCPToken)
	authed.Delete("/admin/users/:id/assistant-tokens", s.requirePeople, s.adminRevokeMCPTokens)
	authed.Get("/messages", s.inbox)
	authed.Get("/messages/:uid", s.messageThread)
	authed.Post("/messages/:uid", s.sendMessage)
	authed.Post("/messages/:uid/read", s.readThread)
	// Group chat: a named conversation with a roster (one for Sales, one for Ops).
	authed.Get("/groups", s.listGroups)
	authed.Post("/groups", s.createGroup)
	authed.Get("/groups/:gid", s.groupThread)
	authed.Post("/groups/:gid", s.sendToGroup)
	authed.Patch("/groups/:gid", s.renameGroup)
	authed.Post("/groups/:gid/read", s.readGroup)
	authed.Post("/groups/:gid/members", s.addGroupMembers)
	authed.Delete("/groups/:gid/members/:uid", s.removeGroupMember)

	authed.Get("/calls/ice", s.callIce)
	authed.Get("/calls/live", s.liveCall)
	authed.Get("/calls", s.recentCalls)
	authed.Post("/calls/:uid", s.startCall)
	authed.Post("/calls/:call_id/status", s.callStatus)
	authed.Post("/calls/:call_id/signal", s.callSignal)

	authed.Get("/notifications", s.listNotifications)
	authed.Get("/notifications/unread-count", s.unreadCount)
	authed.Get("/notifications/stream", s.notificationStream)
	authed.Post("/notifications/read-all", s.readAllNotifications)
	authed.Post("/notifications/:id/read", s.readNotification)

	// Knowledge base / SOP wiki: everyone can read; TL/HR/CEO/admin curate.
	authed.Get("/kb/articles", s.listKbArticles)
	authed.Get("/kb/articles/:slug", s.getKbArticle)
	authed.Post("/kb/articles", s.createKbArticle)
	authed.Put("/kb/articles/:id", s.updateKbArticle)
	authed.Delete("/kb/articles/:id", s.deleteKbArticle)

	// Fiverr review tracker: everyone can view; sales/TL/admin/CEO manage.
	authed.Get("/reviews", s.listReviews)
	authed.Get("/reviews/stats", s.reviewStats)
	authed.Post("/reviews", s.createReview)
	authed.Put("/reviews/:id", s.updateReview)
	authed.Delete("/reviews/:id", s.deleteReview)

	// Project templates: everyone can view and apply; TL/HR/CEO/admin manage.
	authed.Get("/templates", s.listTemplates)
	authed.Get("/templates/:id", s.getTemplate)
	authed.Post("/templates", s.createTemplate)
	authed.Put("/templates/:id", s.updateTemplate)
	authed.Delete("/templates/:id", s.deleteTemplate)
	authed.Post("/templates/:id/apply", s.applyTemplate)

	// Recurring tasks: TL/HR/CEO/admin manage; members can view.
	authed.Get("/recurring", s.listRecurring)
	authed.Post("/recurring", s.createRecurring)
	authed.Put("/recurring/:id", s.updateRecurring)
	authed.Delete("/recurring/:id", s.deleteRecurring)
	authed.Post("/recurring/:id/toggle", s.toggleRecurring)
	authed.Post("/recurring/:id/run-now", s.runRecurringNow)

	// Client billing invoices: sales/admin/ceo/hr manage; TL can view.
	// (Office expense invoices live under /finance/invoices.)
	authed.Get("/billing", s.listInvoices)
	authed.Get("/billing/:id", s.getInvoice)
	authed.Post("/billing", s.createInvoice)
	authed.Put("/billing/:id", s.updateInvoice)
	authed.Delete("/billing/:id", s.deleteInvoice)
	authed.Post("/billing/:id/send", s.sendInvoice)
	authed.Post("/billing/:id/mark-paid", s.markInvoicePaid)

	// Client milestone payments: dated payment schedule per project (starting
	// amount, then milestone payments). Sales/TL/HR/CEO/admin manage.
	authed.Get("/payment-methods", s.paymentMethods)
	authed.Get("/projects/:id/milestones", s.listMilestones)
	authed.Post("/projects/:id/milestones", s.createMilestone)
	authed.Put("/projects/:id/milestones/:mid", s.updateMilestone)
	authed.Delete("/projects/:id/milestones/:mid", s.deleteMilestone)
	authed.Post("/projects/:id/milestones/:mid/record-payment", s.recordMilestonePayment)
	authed.Post("/projects/:id/milestones/:mid/remind", s.remindMilestone)
	authed.Get("/milestones/due", s.dueMilestones)
	authed.Get("/milestones/:mid", s.getMilestone)

	// Investor Finance Forum: capital partners, shares, distributions,
	// withdrawals and investor-tracked expenses. CEO/HR/admin manage; an
	// investor sees only their own profile via /investor/me.
	authed.Get("/investors", s.listInvestors)
	authed.Post("/investors", s.createInvestor)
	authed.Get("/investors/:id", s.getInvestor)
	authed.Put("/investors/:id", s.updateInvestor)
	authed.Post("/investors/:id/investments", s.recordInvestment)
	authed.Put("/investors/:id/share", s.setShare)
	authed.Post("/investors/:id/distribute", s.distributeManual)
	authed.Post("/investors/:id/withdrawals", s.requestWithdrawal)
	authed.Get("/withdrawals", s.listWithdrawals)
	authed.Put("/withdrawals/:id/approve", func(c fiber.Ctx) error { return s.reviewWithdrawal(c, "approved") })
	authed.Put("/withdrawals/:id/reject", func(c fiber.Ctx) error { return s.reviewWithdrawal(c, "rejected") })
	authed.Put("/withdrawals/:id/mark-paid", func(c fiber.Ctx) error { return s.reviewWithdrawal(c, "paid") })
	authed.Get("/profit-settings", s.getProfitSettings)
	authed.Put("/profit-settings", s.updateProfitSettings)
	authed.Get("/company-account", s.companyAccount)
	authed.Get("/investor-expenses", s.listInvestorExpenses)
	authed.Post("/investor-expenses", s.createInvestorExpense)
	authed.Delete("/investor-expenses/:id", s.deleteInvestorExpense)
	authed.Get("/currency-rates", s.listCurrencyRates)
	authed.Post("/currency-rates", s.createCurrencyRate)
	authed.Get("/currency-rates/latest", s.latestCurrencyRate)
	authed.Get("/investor/me", requireInvestor, s.investorMe)

	// Oversight: CEO dashboard and exports; activity for TL + CEO.
	authed.Get("/reports/summary", requireCan(policy.ViewDashboard), s.reportSummary)
	authed.Get("/reports/workload", requireCan(policy.ViewWorkload), s.reportWorkload)
	authed.Get("/reports/company", requireCan(policy.ViewDashboard), s.reportCompany)
	authed.Get("/reports/export/updates.csv", requireCan(policy.ExportCSV), s.exportUpdates)
	authed.Get("/reports/export/projects.csv", requireCan(policy.ExportCSV), s.exportProjects)
	authed.Get("/activity", requireCan(policy.ViewActivity), s.listActivity)
	authed.Get("/analytics", s.requireAnalytics, s.analytics)
	authed.Get("/analytics/projects", s.requireAnalytics, s.analyticsProjects)
	authed.Get("/analytics/export.csv", s.requireAnalytics, s.analyticsExport)
	authed.Get("/analytics/trend", s.requireAnalytics, s.analyticsTrend)
	// Revenue targets per team and month: everyone with Analytics sees them, the CEO sets them.
	authed.Get("/analytics/targets", s.requireAnalytics, s.analyticsTargets)
	authed.Put("/analytics/targets/:team_id/:month", s.requireAnalytics, s.setTeamTarget)
	authed.Delete("/analytics/targets/:team_id/:month", s.requireAnalytics, s.clearTeamTarget)

	// Insights: revenue forecast + performance leaderboard (read-only analytics
	// for TLs, HR, the CEO and system admins).
	authed.Get("/insights/revenue-forecast", requireInsights, s.revenueForecast)
	authed.Get("/insights/leaderboard", requireInsights, s.leaderboard)

	// Client portal: limited project visibility for external clients.
	authed.Get("/client/projects", requireClient, s.clientProjects)
	authed.Get("/client/projects/:id", requireClient, s.clientProject)
	authed.Get("/client/projects/:id/updates", requireClient, s.clientProjectUpdates)
	authed.Get("/client/projects/:id/feedback", requireClient, s.clientFeedback)
	authed.Post("/client/projects/:id/feedback", requireClient, s.clientSubmitFeedback)
	authed.Get("/client/projects/:id/milestones", requireClient, s.clientMilestones)

	// Client collaboration (Phase 4): ClickUp-style work with the client.
	// Business clients, per-project links, shared task comments + approvals,
	// per-project messages and shared files. Team routes are role-gated in
	// the handlers; portal routes sit under /client and check the login link.
	authed.Get("/clients", s.listClients)
	authed.Post("/clients", s.createClient)
	authed.Get("/clients/:id", s.getClient)
	authed.Put("/clients/:id", s.updateClient)
	authed.Post("/clients/:id/contacts", s.addClientContact)
	authed.Delete("/clients/:id/contacts/:cid", s.deleteClientContact)
	authed.Get("/projects/:id/collab", s.projectCollab)
	authed.Post("/projects/:id/link-client", s.linkClient)
	authed.Delete("/projects/:id/link-client/:cid", s.unlinkClient)
	authed.Get("/projects/:id/cards/:cardId/comments", s.listTaskComments)
	authed.Post("/projects/:id/cards/:cardId/comments", s.addTaskComment)
	authed.Put("/projects/:id/cards/:cardId/visible", s.setCardVisible)
	authed.Get("/projects/:id/cards/:cardId/approvals", s.listTaskApprovals)
	authed.Post("/projects/:id/cards/:cardId/request-approval", s.requestTaskApproval)
	authed.Get("/projects/:id/messages", s.listClientMessages)
	authed.Post("/projects/:id/messages", s.addClientMessage)
	authed.Get("/projects/:id/files", s.listClientFiles)
	authed.Post("/projects/:id/files", s.addClientFile)
	authed.Post("/projects/:id/collab-files/uploads", s.initClientFileUpload)
	authed.Post("/projects/:id/collab-files/uploads/:upload_id/complete", s.completeClientFileUpload)
	authed.Get("/client-files/:id/download", s.downloadClientFile)
	authed.Get("/client/projects/:id/collab", requireClient, s.clientCollab)
	authed.Get("/client/projects/:id/cards/:cardId/comments", requireClient, s.listTaskComments)
	authed.Post("/client/projects/:id/cards/:cardId/comments", requireClient, s.addTaskComment)
	authed.Post("/client/projects/:id/cards/:cardId/review", requireClient, s.reviewTaskApproval)
	authed.Get("/client/projects/:id/messages", requireClient, s.listClientMessages)
	authed.Post("/client/projects/:id/messages", requireClient, s.addClientMessage)
	authed.Get("/client/projects/:id/files", requireClient, s.listClientFiles)
	authed.Post("/client/projects/:id/collab-files/uploads", requireClient, s.initClientFileUpload)
	authed.Post("/client/projects/:id/collab-files/uploads/:upload_id/complete", requireClient, s.completeClientFileUpload)

	// B2B project features (Phase 5): contracts, retainers, per-milestone
	// teams + SLA, and the B2B report. Sales/admin manage; TL/CEO/HR read.
	authed.Get("/contracts", s.listContracts)
	authed.Post("/contracts", s.createContract)
	authed.Get("/contracts/:id", s.getContract)
	authed.Put("/contracts/:id", s.updateContract)
	authed.Post("/contracts/:id/renew", s.renewContract)
	authed.Get("/retainers", s.listRetainers)
	authed.Post("/retainers", s.createRetainer)
	authed.Get("/retainers/due", s.dueRetainers)
	authed.Get("/retainers/:id", s.getRetainer)
	authed.Put("/retainers/:id", s.updateRetainer)
	authed.Put("/projects/:id/milestones/:mid/team", s.setMilestoneTeam)
	authed.Get("/b2b/report", s.b2bReport)

	// Deadline alerts: urgency-sorted upcoming/past due dates + hourly
	// due-soon/overdue notifications (see projects.Service.RunChecks).
	authed.Get("/alerts/deadlines", s.deadlines)

	// Time tracking: users run one timer at a time; TL/HR/CEO/admins can view all.
	authed.Post("/time/start", s.startTimer)
	authed.Post("/time/stop", s.stopTimer)
	authed.Get("/time/running", s.runningTimer)
	authed.Get("/time/entries", s.listTimeEntries)
	authed.Delete("/time/:id", s.deleteTimeEntry)

	// Client updates: visibility and approval rules live in updateflow + the projects service.
	authed.Get("/updates", s.listUpdates)
	authed.Post("/projects/:id/updates", s.createUpdate)
	authed.Get("/updates/:uid", s.getUpdate)
	authed.Patch("/updates/:uid", s.withUpdate(s.editUpdate))
	authed.Post("/updates/:uid/submit", s.withUpdate(s.submitUpdate))
	authed.Post("/updates/:uid/approve", s.withUpdate(s.approveUpdate))
	authed.Post("/updates/:uid/request-changes", s.withUpdate(s.requestUpdateChanges))
	authed.Post("/updates/:uid/sent", s.withUpdate(s.markUpdateSent))
	authed.Post("/updates/:uid/comments", s.withUpdate(s.commentUpdate))
	authed.Post("/updates/:uid/attachments/uploads", s.initUpdateUpload)
	authed.Post("/updates/:uid/attachments/uploads/:upload_id/complete", s.withUpdate(s.completeUpdateUpload))
	authed.Delete("/updates/:uid/attachments/:file_id", s.withUpdate(s.removeUpdateAttachment))

	// HRM. Self service lives under /hr/me; the hr service scopes everything else to the
	// person, their reporting line, or HR and the CEO.
	authed.Get("/hr/me", s.hrMe)
	authed.Put("/hr/me/profile", s.hrUpdateMyProfile)
	authed.Post("/hr/me/check-in", s.hrCheckIn)
	authed.Post("/hr/me/check-out", s.hrCheckOut)
	authed.Get("/hr/me/corrections", s.hrMyCorrections)
	authed.Post("/hr/me/corrections", s.hrRequestCorrection)
	authed.Post("/hr/me/corrections/:id/cancel", s.hrCancelCorrection)
	authed.Post("/hr/me/leave", s.hrApplyLeave)
	authed.Post("/hr/me/leave/:id/cancel", s.hrCancelLeave)
	authed.Get("/hr/directory", s.hrDirectory)
	authed.Get("/hr/employees/:id", s.hrGetEmployee)
	authed.Put("/hr/employees/:id", s.hrUpdateEmployee)
	authed.Get("/hr/employees/:id/balances", s.hrBalances)
	authed.Get("/hr/attendance", s.hrAttendance)
	authed.Get("/hr/attendance/export.csv", s.hrExportAttendance)
	authed.Get("/hr/leave", s.hrListLeave)
	authed.Get("/hr/whos-out", s.hrWhosOut)
	authed.Get("/hr/approvals", s.hrApprovals)
	authed.Post("/hr/leave/:id/approve", s.hrReview("leave", true))
	authed.Post("/hr/leave/:id/reject", s.hrReview("leave", false))
	authed.Post("/hr/corrections/:id/approve", s.hrReview("correction", true))
	authed.Post("/hr/corrections/:id/reject", s.hrReview("correction", false))
	authed.Get("/hr/leave-types", s.hrLeaveTypes)
	authed.Get("/hr/departments", s.hrDepartments)
	authed.Get("/hr/holidays", s.hrHolidays)
	authed.Get("/hr/shifts", s.hrShifts)
	authed.Get("/hr/salaries", s.requirePeople, s.hrSalaries)
	authed.Get("/hr/employees/:id/salary", s.requirePeople, s.hrSalaryHistory)
	authed.Post("/hr/employees/:id/salary", s.requirePeople, s.hrAddSalary)
	authed.Delete("/hr/salaries/:id", s.requirePeople, s.hrDeleteSalary)

	// Finance (HR + CEO): the ledger, its categories and rates, and the monthly analytics.
	authed.Get("/finance/summary", s.requirePeople, s.financeSummary)
	authed.Get("/finance/entries", s.requirePeople, s.financeEntries)
	authed.Get("/finance/entries/export.csv", s.requirePeople, s.financeExport)
	authed.Post("/finance/entries", s.requirePeople, s.financeCreateEntry)
	authed.Patch("/finance/entries/:id", s.requirePeople, s.financeUpdateEntry)
	authed.Delete("/finance/entries/:id", s.requirePeople, s.financeDeleteEntry)
	authed.Get("/finance/categories", s.requirePeople, s.financeCategories)
	authed.Post("/finance/categories", s.requirePeople, s.financeCreateCategory)
	authed.Patch("/finance/categories/:id", s.requirePeople, s.financeUpdateCategory)
	authed.Get("/finance/rates", s.requirePeople, s.financeRates)
	// The invoice form needs the rate it will use, so invoice raisers may read this one.
	authed.Get("/finance/rates/on", requireCan(policy.InvoiceRaise), s.financeRateOn)
	authed.Post("/finance/rates", s.requirePeople, s.financeAddRate)
	authed.Delete("/finance/rates/:id", s.requirePeople, s.financeDeleteRate)
	// Expense invoices: an Office Admin raises and sees their own; HR and the CEO see every
	// one and decide at their step. The finance service decides whose step it is.
	authed.Get("/finance/invoices", requireCan(policy.InvoiceRaise), s.financeInvoices)
	authed.Post("/finance/invoices", requireCan(policy.InvoiceRaise), s.financeCreateInvoice)
	authed.Get("/finance/invoices/:id", requireCan(policy.InvoiceRaise), s.financeInvoice)
	authed.Patch("/finance/invoices/:id", requireCan(policy.InvoiceRaise), s.financeUpdateInvoice)
	authed.Post("/finance/invoices/:id/cancel", requireCan(policy.InvoiceRaise), s.financeCancelInvoice)
	authed.Post("/finance/invoices/:id/approve", requireCan(policy.InvoiceReview), s.financeReviewInvoice(true))
	authed.Post("/finance/invoices/:id/reject", requireCan(policy.InvoiceReview), s.financeReviewInvoice(false))

	authed.Get("/finance/payroll", s.requirePeople, s.financePayroll)
	authed.Put("/finance/payroll/:user_id/:month", s.requirePeople, s.financeSavePayment)
	authed.Delete("/finance/payroll/:id", s.requirePeople, s.financeDeletePayment)

	// People (HR + CEO): accounts, roles and HR configuration. Middleware is per route:
	// a Fiber group would apply to every route under the shared /admin prefix.
	authed.Get("/admin/users", s.requirePeople, s.adminListUsers)
	authed.Post("/admin/users", s.requirePeople, s.adminCreateUser)
	authed.Patch("/admin/users/:id", s.requirePeople, s.adminUpdateUser)
	authed.Post("/admin/users/:id/reset-password", s.requirePeople, s.adminResetPassword)
	authed.Post("/admin/departments", s.requirePeople, s.adminCreateDepartment)
	authed.Patch("/admin/departments/:id", s.requirePeople, s.adminUpdateDepartment)
	authed.Post("/admin/leave-types", s.requirePeople, s.adminCreateLeaveType)
	authed.Patch("/admin/leave-types/:id", s.requirePeople, s.adminUpdateLeaveType)
	authed.Post("/admin/holidays", s.requirePeople, s.adminCreateHoliday)
	authed.Delete("/admin/holidays/:id", s.requirePeople, s.adminDeleteHoliday)
	authed.Put("/admin/leave-allocations", s.requirePeople, s.adminSetAllocation)
	authed.Post("/admin/shifts", s.requirePeople, s.adminCreateShift)
	authed.Post("/admin/shifts/assign", s.requirePeople, s.adminAssignShift)
	authed.Patch("/admin/shifts/:id", s.requirePeople, s.adminUpdateShift)
	authed.Get("/admin/teams/:id", s.requirePeople, s.adminGetTeam)
	authed.Post("/admin/teams", s.requirePeople, s.adminCreateTeam)
	authed.Patch("/admin/teams/:id", s.requirePeople, s.adminUpdateTeam)
	authed.Put("/admin/teams/:id/members/:user_id", s.requirePeople, s.adminAddTeamMember)
	authed.Delete("/admin/teams/:id/members/:user_id", s.requirePeople, s.adminRemoveTeamMember)

	// Settings: HR keys for HR + CEO, the rest for system admins (checked per key).
	authed.Put("/admin/settings/:key", s.adminUpdateSetting)

	// System setup (admin flag + CEO).
	authed.Get("/admin/profiles", s.requireAdmin, s.adminListProfiles)
	authed.Post("/admin/profiles", s.requireAdmin, s.adminCreateProfile)
	authed.Patch("/admin/profiles/:id", s.requireAdmin, s.adminUpdateProfile)
	authed.Get("/admin/project-types", s.requireAdmin, s.adminListProjectTypes)
	authed.Post("/admin/project-types", s.requireAdmin, s.adminCreateProjectType)
	authed.Patch("/admin/project-types/:id", s.requireAdmin, s.adminUpdateProjectType)

	// Google Sheet backup (system setup).
	authed.Get("/admin/sheet", s.requireAdmin, s.sheetStatus)
	authed.Post("/admin/sheet/check", s.requireAdmin, s.sheetCheck)
	authed.Post("/admin/sheet/backfill", s.requireAdmin, s.sheetBackfill)

	return app, s.projects, s.backup, s.chat
}

func (s *Server) health(c fiber.Ctx) error {
	if err := s.store.Pool.Ping(c.Context()); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "db_down"})
	}
	if err := s.rdb.Ping(c.Context()).Err(); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "redis_down"})
	}
	if err := s.files.Ping(c.Context()); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "storage_down"})
	}
	return c.JSON(fiber.Map{"status": "ok"})
}
