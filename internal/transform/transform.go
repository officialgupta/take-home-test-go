package transform

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"take-home-test-go/internal/forms"
	"take-home-test-go/internal/providers"
)

// SplitName splits a full name into first/last name. There is no canonical
// way to do this for a multi-part name (e.g. "Andy James Smith-Jones") — this
// is a deliberate, documented simplification: the first whitespace-delimited
// token is the first name, everything else is the last name.
func SplitName(name string) (first, last string) {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return "", ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}

// MapGender maps the ingested gender enum to the transformed one. "other" is
// mapped to "prefer-not-to-say" — the task brief never states this mapping
// explicitly, but it's the only sane 1:1 inference given the enums don't
// otherwise line up.
func MapGender(gender string) (string, error) {
	switch gender {
	case "male", "female":
		return gender, nil
	case "other":
		return "prefer-not-to-say", nil
	default:
		return "", fmt.Errorf("unrecognized gender %q", gender)
	}
}

// dateOfBirthLayout is the one accepted date_of_birth format.
const dateOfBirthLayout = "2006-01-02"

func ParseDateOfBirth(s string) (time.Time, error) {
	t, err := time.Parse(dateOfBirthLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date_of_birth %q: %w", s, err)
	}
	return t, nil
}

// fieldCollector accumulates validation errors in call order, so Validate's
// joined error message stays deterministic.
type fieldCollector struct{ errs []error }

func (c *fieldCollector) require(field, value string) {
	if strings.TrimSpace(value) == "" {
		c.errs = append(c.errs, fmt.Errorf("%s is required", field))
	}
}

// Validate reports every way in, taken as a whole, fails to conform to the
// agreed ingested schema: blank required fields, an unrecognized gender, or
// an unparseable date_of_birth. It does no geocoding and builds no output —
// callers use it to fail fast on bad data before paying for a geocode call,
// and MapForm uses it as its own precondition, so there is exactly one place
// that knows what "valid" means.
func Validate(in forms.IngestedForm) error {
	c := &fieldCollector{}
	c.require("name", in.Name)
	c.require("email", in.Email)
	c.require("mobile_number", in.MobileNumber)
	c.require("address.address_line_1", in.Address.AddressLine1)
	c.require("address.address_line_2", in.Address.AddressLine2)
	c.require("address.postcode", in.Address.Postcode)
	c.require("address.country", in.Address.Country)

	if _, err := MapGender(in.Gender); err != nil {
		c.errs = append(c.errs, err)
	}
	if _, err := ParseDateOfBirth(in.DateOfBirth); err != nil {
		c.errs = append(c.errs, err)
	}
	return errors.Join(c.errs...)
}

// MapForm assembles a TransformedForm from an IngestedForm and a resolved
// geocode result. It re-validates in via Validate first — cheap relative to
// the geocode call that already happened to produce geo — so a caller can
// never accidentally map an invalid record just by skipping the check.
func MapForm(in forms.IngestedForm, geo providers.GeoResult) (forms.TransformedForm, error) {
	if err := Validate(in); err != nil {
		return forms.TransformedForm{}, err
	}

	first, last := SplitName(in.Name)
	gender, _ := MapGender(in.Gender)          // already validated above
	dob, _ := ParseDateOfBirth(in.DateOfBirth) // already validated above

	return forms.TransformedForm{
		SessionID:            in.SessionID,
		ApplicationReference: in.ApplicationReference,
		FirstName:            first,
		LastName:             last,
		Email:                in.Email,
		Gender:               gender,
		DateOfBirth:          dob,
		PhoneNumber:          in.PhoneNumber,
		MobileNumber:         in.MobileNumber,
		AddressLine1:         in.Address.AddressLine1,
		AddressLine2:         in.Address.AddressLine2,
		AddressLine3:         in.Address.AddressLine3,
		Postcode:             in.Address.Postcode,
		Country:              in.Address.Country,
		Longitude:            geo.Longitude,
		Latitude:             geo.Latitude,
	}, nil
}
