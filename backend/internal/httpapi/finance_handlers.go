package httpapi

import (
	"bytes"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/finance"
)

// ── Finance (HR + CEO): ledger, categories, rates and analytics ──

func financeFilter(c fiber.Ctx) finance.EntryFilter {
	return finance.EntryFilter{
		From: c.Query("from"), To: c.Query("to"), Kind: c.Query("kind"), CategoryID: c.Query("category_id"), Q: c.Query("q"),
	}
}

func (s *Server) financeSummary(c fiber.Ctx) error {
	sum, err := s.finance.Summary(c.Context(), currentUser(c), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	return c.JSON(sum)
}

func (s *Server) financeEntries(c fiber.Ctx) error {
	rows, from, to, err := s.finance.Entries(c.Context(), currentUser(c), financeFilter(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"entries": rows, "from": from, "to": to})
}

func (s *Server) financeExport(c fiber.Ctx) error {
	var buf bytes.Buffer
	from, to, err := s.finance.WriteEntriesCSV(c.Context(), &buf, currentUser(c), financeFilter(c))
	if err != nil {
		return err
	}
	return sendCSV(c, "finance_"+from+"_"+to+".csv", &buf)
}

func (s *Server) financeCreateEntry(c fiber.Ctx) error {
	var req finance.EntryInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	id, err := s.finance.CreateEntry(c.Context(), currentUser(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func (s *Server) financeUpdateEntry(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req finance.EntryInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.finance.UpdateEntry(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) financeDeleteEntry(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.finance.DeleteEntry(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) financeCategories(c fiber.Ctx) error {
	rows, err := s.finance.Categories(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"categories": rows})
}

func (s *Server) financeCreateCategory(c fiber.Ctx) error {
	var req finance.CategoryInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.finance.CreateCategory(c.Context(), currentUser(c), req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusCreated)
}

func (s *Server) financeUpdateCategory(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req finance.CategoryInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.finance.UpdateCategory(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) financeRates(c fiber.Ctx) error {
	rows, err := s.finance.Rates(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"rates": rows})
}

// financeRateOn answers the entry form: the rate it will use for a currency on a day.
func (s *Server) financeRateOn(c fiber.Ctx) error {
	rate, err := s.finance.RateOn(c.Context(), currentUser(c), c.Query("currency"), c.Query("date"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"rate": rate})
}

func (s *Server) financeAddRate(c fiber.Ctx) error {
	var req finance.RateInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.finance.AddRate(c.Context(), currentUser(c), req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusCreated)
}

func (s *Server) financeDeleteRate(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.finance.DeleteRate(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Payroll: each person's pay for a month, with bonuses and deductions ──

func (s *Server) financePayroll(c fiber.Ctx) error {
	sheet, err := s.finance.Payroll(c.Context(), currentUser(c), c.Query("month"))
	if err != nil {
		return err
	}
	return c.JSON(sheet)
}

func (s *Server) financeSavePayment(c fiber.Ctx) error {
	userID, err := paramUUID(c, "user_id")
	if err != nil {
		return err
	}
	var req finance.PaymentInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.finance.SavePayment(c.Context(), currentUser(c), userID, c.Params("month"), req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) financeDeletePayment(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.finance.DeletePayment(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Expense invoices: raised by anyone who may, approved by HR then the CEO ──

func (s *Server) financeInvoices(c fiber.Ctx) error {
	out, err := s.finance.Invoices(c.Context(), currentUser(c), finance.InvoiceFilter{
		Status: c.Query("status"), From: c.Query("from"), To: c.Query("to"),
		Mine: c.Query("mine") == "true", Q: c.Query("q"),
	})
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) financeInvoice(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	inv, err := s.finance.Invoice(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(inv)
}

func (s *Server) financeCreateInvoice(c fiber.Ctx) error {
	var req finance.InvoiceInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	inv, err := s.finance.CreateInvoice(c.Context(), currentUser(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(inv)
}

func (s *Server) financeUpdateInvoice(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req finance.InvoiceInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	inv, err := s.finance.UpdateInvoice(c.Context(), currentUser(c), id, req)
	if err != nil {
		return err
	}
	return c.JSON(inv)
}

func (s *Server) financeCancelInvoice(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	inv, err := s.finance.CancelInvoice(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(inv)
}

// financeReviewInvoice is one step's decision; who may take it depends on where the invoice
// has got to, which the finance service decides.
func (s *Server) financeReviewInvoice(approve bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := paramUUID(c, "id")
		if err != nil {
			return err
		}
		note, err := optionalNote(c)
		if err != nil {
			return err
		}
		inv, err := s.finance.ReviewInvoice(c.Context(), currentUser(c), id, approve, note)
		if err != nil {
			return err
		}
		return c.JSON(inv)
	}
}
