package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/solvinx/opsflow/internal/notify"
)

// nextRecurringRun computes the next run date after the given date,
// based on the frequency and day settings.
func nextRecurringRun(frequency string, dayOfWeek, dayOfMonth *int, after time.Time) time.Time {
	switch frequency {
	case "daily":
		return after.AddDate(0, 0, 1)
	case "weekly":
		dow := 1 // default Monday
		if dayOfWeek != nil {
			dow = *dayOfWeek
		}
		// Days until next occurrence of dow (0=Sunday..6=Saturday).
		days := (dow - int(after.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		return after.AddDate(0, 0, days)
	case "monthly":
		dom := 1
		if dayOfMonth != nil {
			dom = *dayOfMonth
		}
		// Next month, same day (clamped to month length).
		y, m, _ := after.Date()
		m++
		if m > 12 {
			m, y = 1, y+1
		}
		lastDay := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
		if dom > lastDay {
			dom = lastDay
		}
		return time.Date(y, m, dom, 0, 0, 0, 0, time.UTC)
	default:
		return after.AddDate(0, 0, 1)
	}
}

// runRecurringTask executes one recurring task: creates a board card in the
// linked project (or a standalone notification when no project), advances the
// schedule, and notifies the creator. Exported for the background checker.
//
// Sprint 1 (P0-1): the schedule advance is a conditional claim —
// UPDATE ... WHERE id AND next_run = <the value we read> — inside the same
// transaction as the card insert. Two runners racing (or a restart between
// the insert and the advance) can no longer create the same card twice: the
// loser claims 0 rows and rolls back without inserting anything.
func runRecurringTask(ctx context.Context, pool *pgxpool.Pool, taskID uuid.UUID, actorID uuid.UUID) error {
	var title, description, frequency string
	var projectID *uuid.UUID
	var dayOfWeek, dayOfMonth *int
	var createdBy *uuid.UUID
	var nextRun time.Time
	err := pool.QueryRow(ctx, `
		SELECT title, description, frequency, project_id, day_of_week, day_of_month, created_by, next_run
		FROM recurring_tasks WHERE id = $1 AND is_active = true
	`, taskID).Scan(&title, &description, &frequency, &projectID, &dayOfWeek, &dayOfMonth, &createdBy, &nextRun)
	if err != nil {
		return fmt.Errorf("recurring task not found or inactive: %w", err)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	next := nextRecurringRun(frequency, dayOfWeek, dayOfMonth, today)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Claim this run first: only the runner whose expected next_run still
	// matches proceeds. A concurrent runner (or a re-run after a crash)
	// claims 0 rows and stops here, before any card exists.
	ct, err := tx.Exec(ctx, `
		UPDATE recurring_tasks SET last_run = $2::date, next_run = $3::date, updated_at = now()
		WHERE id = $1 AND next_run = $4::date
	`, taskID, today.Format("2006-01-02"), next.Format("2006-01-02"), nextRun.Format("2006-01-02"))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return nil
	}

	if projectID != nil {
		// Create a board card in the project's To-do column.
		var number int
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(number), 0) + 1 FROM project_cards WHERE project_id = $1
		`, *projectID).Scan(&number)
		if err != nil {
			return err
		}
		var cardID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO project_cards (project_id, number, title, description, source, column_key, created_by)
			VALUES ($1, $2, $3, $4, 'internal', 'todo', $5)
			RETURNING id
		`, *projectID, number, title, description, actorID).Scan(&cardID)
		if err != nil {
			return err
		}
	}

	// Notify the creator.
	if createdBy != nil && *createdBy != uuid.Nil {
		_, _ = tx.Exec(ctx, `
			INSERT INTO notifications (recipient_id, type, project_id, title, body, data)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		`, *createdBy, notify.RecurringRan, projectID, "Recurring task ran: "+title,
			fmt.Sprintf("Created for %s (%s schedule).", today.Format("Jan 2"), frequency),
			`{}`)
	}
	return tx.Commit(ctx)
}

// ProcessDueRecurringTasks runs every active recurring task whose next_run is
// today or earlier. Called from the scheduled checks.
func ProcessDueRecurringTasks(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT id, created_by FROM recurring_tasks
		WHERE is_active = true AND next_run <= CURRENT_DATE
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type due struct {
		id        uuid.UUID
		createdBy *uuid.UUID
	}
	var tasks []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.createdBy); err != nil {
			return err
		}
		tasks = append(tasks, d)
	}
	rows.Close()
	for _, t := range tasks {
		actor := uuid.Nil
		if t.createdBy != nil {
			actor = *t.createdBy
		}
		if err := runRecurringTask(ctx, pool, t.id, actor); err != nil {
			// Log and continue with the next task.
			continue
		}
	}
	return nil
}
