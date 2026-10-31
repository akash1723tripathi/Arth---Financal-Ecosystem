package httpapi

// Shared converters from sqlc rows (internal/db) to generated API models (internal/httpapi/gen).
// Every domain handler uses these so a resource has exactly one JSON shape.

import (
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"arth/api/internal/db"
	"arth/api/internal/httpapi/gen"
)

func toMe(u db.User) gen.Me {
	return gen.Me{
		Id:                u.ID,
		Email:             u.Email,
		EmailVerified:     u.EmailVerifiedAt != nil,
		Name:              u.Name,
		AvatarUrl:         nil, // object storage is OPEN (D-OPEN-7)
		Currency:          u.Currency,
		Timezone:          u.Timezone,
		NeedsOnboarding:   u.OnboardedAt == nil,
		EmailBudgetAlerts: u.EmailBudgetAlerts,
		EmailWeeklyTips:   u.EmailWeeklyTips,
		CreatedAt:         u.CreatedAt.UTC(),
	}
}

func toAccount(a db.Account) gen.Account {
	return gen.Account{
		Id:                  a.ID,
		Name:                a.Name,
		Type:                gen.AccountType(a.Type),
		Currency:            a.Currency,
		BalanceMinor:        a.BalanceMinor,
		OpeningBalanceMinor: a.OpeningBalanceMinor,
		IsDefault:           a.IsDefault,
		CreatedAt:           a.CreatedAt.UTC(),
	}
}

func toAccounts(rows []db.Account) []gen.Account {
	out := make([]gen.Account, 0, len(rows))
	for _, a := range rows {
		out = append(out, toAccount(a))
	}
	return out
}

func toCategory(c db.Category) gen.Category {
	return gen.Category{
		Id:       c.ID,
		Key:      c.Key,
		Name:     c.Name,
		Kind:     gen.EntryType(c.Kind),
		Icon:     c.Icon,
		Color:    c.Color,
		IsSystem: c.UserID == nil,
	}
}

func toTransaction(t db.Transaction) gen.Transaction {
	return gen.Transaction{
		Id:              t.ID,
		AccountId:       t.AccountID,
		CategoryId:      t.CategoryID,
		Type:            gen.TransactionType(t.Type),
		AmountMinor:     t.AmountMinor,
		Currency:        t.Currency,
		OccurredOn:      toDate(t.OccurredOn),
		Description:     t.Description,
		InputMethod:     gen.InputMethod(t.InputMethod),
		VoiceTranscript: t.VoiceTranscript,
		TransferId:      t.TransferID,
		CreatedAt:       t.CreatedAt.UTC(),
		UpdatedAt:       t.UpdatedAt.UTC(),
	}
}

func toTransactions(rows []db.Transaction) []gen.Transaction {
	out := make([]gen.Transaction, 0, len(rows))
	for _, t := range rows {
		out = append(out, toTransaction(t))
	}
	return out
}

// toDate converts a DATE column (time.Time at midnight UTC) to the API date type.
func toDate(t time.Time) openapi_types.Date {
	return openapi_types.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
}

// fromDate converts an API date to the time.Time sqlc expects for DATE columns.
func fromDate(d openapi_types.Date) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}
