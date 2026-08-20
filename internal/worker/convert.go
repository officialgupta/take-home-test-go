package worker

import (
	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/forms"
	"take-home-test-go/internal/store"
)

func toInsertParams(ingestedFormID int64, out forms.TransformedForm) store.InsertTransformedFormParams {
	return store.InsertTransformedFormParams{
		IngestedFormID:       ingestedFormID,
		ApplicationReference: out.ApplicationReference,
		SessionID:            out.SessionID,
		FirstName:            out.FirstName,
		LastName:             out.LastName,
		Email:                out.Email,
		Gender:               out.Gender,
		DateOfBirth:          pgtype.Date{Time: out.DateOfBirth, Valid: true},
		PhoneNumber:          strPtrToText(out.PhoneNumber),
		MobileNumber:         out.MobileNumber,
		AddressLine1:         out.AddressLine1,
		AddressLine2:         out.AddressLine2,
		AddressLine3:         strPtrToText(out.AddressLine3),
		Postcode:             out.Postcode,
		Country:              out.Country,
		Longitude:            out.Longitude,
		Latitude:             out.Latitude,
	}
}

func strPtrToText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}
