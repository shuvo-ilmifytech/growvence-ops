package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/db"
)

func (s *Server) listNotifications(c fiber.Ctx) error {
	u := currentUser(c)
	limit, _ := strconv.Atoi(c.Query("limit", "30"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit < 1 || limit > 100 || offset < 0 {
		return errBadRequest("limit must be 1-100 and offset >= 0")
	}
	rows, err := s.store.ListNotifications(c.Context(), db.ListNotificationsParams{
		RecipientID: u.ID, UnreadOnly: c.Query("unread") == "true", Lim: int32(limit), Off: int32(offset),
	})
	if err != nil {
		return err
	}
	unread, err := s.store.CountUnreadNotifications(c.Context(), u.ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"notifications": rows, "unread_count": unread})
}

func (s *Server) readNotification(c fiber.Ctx) error {
	u := currentUser(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return errBadRequest("Invalid id")
	}
	if _, err := s.store.MarkNotificationRead(c.Context(), db.MarkNotificationReadParams{ID: id, RecipientID: u.ID}); err != nil {
		return err
	}
	return s.unreadCount(c)
}

func (s *Server) readAllNotifications(c fiber.Ctx) error {
	if _, err := s.store.MarkAllNotificationsRead(c.Context(), currentUser(c).ID); err != nil {
		return err
	}
	return s.unreadCount(c)
}

func (s *Server) unreadCount(c fiber.Ctx) error {
	n, err := s.store.CountUnreadNotifications(c.Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"unread_count": n})
}

// notificationStream is a Server-Sent Events stream of the user's new notifications.
// Each event carries the notification and the fresh unread count.
func (s *Server) notificationStream(c fiber.Ctx) error {
	u := currentUser(c)
	ch, unsubscribe := s.hub.Subscribe(u.ID)

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache, no-transform")
	c.Set(fiber.HeaderConnection, "keep-alive")
	c.Set("X-Accel-Buffering", "no") // nginx: don't buffer

	// The same stream carries chat messages and the back-and-forth of a voice call.
	liveEvents, stopLive := s.live.Subscribe(context.Background(), u.ID)

	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer unsubscribe()
		defer stopLive()
		// The request context ends when the handler returns; the stream outlives it.
		ctx := context.Background()
		send := func(event string, v any) bool {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
			return w.Flush() == nil // an error means the client went away
		}
		unread, _ := s.store.CountUnreadNotifications(ctx, u.ID)
		if !send("ready", fiber.Map{"unread_count": unread}) {
			return
		}
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case id := <-ch:
				n, err := s.store.GetNotification(ctx, id)
				if err != nil || n.RecipientID != u.ID {
					continue
				}
				unread, _ := s.store.CountUnreadNotifications(ctx, u.ID)
				if !send("notification", fiber.Map{"notification": n, "unread_count": unread}) {
					return
				}
			case e := <-liveEvents:
				if !send(e.Kind, e.Data) {
					return
				}
			case <-ping.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || w.Flush() != nil {
					return
				}
			}
		}
	})
}
