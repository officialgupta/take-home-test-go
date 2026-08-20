package transform

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"take-home-test-go/internal/forms"
	"take-home-test-go/internal/providers"
)

func TestSplitName(t *testing.T) {
	cases := []struct {
		name           string
		wantFirst      string
		wantLastPrefix string // exact match, named for clarity below
	}{
		{"John Doe", "John", "Doe"},
		{"Madonna", "Madonna", ""},
		{"Andy James Smith-Jones", "Andy", "James Smith-Jones"},
		{"", "", ""},
		{"  Extra   Spaces  Here ", "Extra", "Spaces Here"}, // strings.Fields collapses runs of whitespace
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first, last := SplitName(c.name)
			if first != c.wantFirst || last != c.wantLastPrefix {
				t.Errorf("SplitName(%q) = (%q, %q), want (%q, %q)", c.name, first, last, c.wantFirst, c.wantLastPrefix)
			}
		})
	}
}

func TestMapGender(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"male", "male", false},
		{"female", "female", false},
		{"other", "prefer-not-to-say", false},
		{"", "", true},
		{"nonbinary", "", true},
		{"Male", "", true}, // case-sensitive: schema drift shouldn't be silently guessed at
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := MapGender(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("MapGender(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("MapGender(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestParseDateOfBirth(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"1990-01-01", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"1921-03-14", time.Date(1921, 3, 14, 0, 0, 0, 0, time.UTC), false},
		{"", time.Time{}, true},
		{"not-a-date", time.Time{}, true},
		{"01/02/1990", time.Time{}, true}, // wrong layout, not silently reinterpreted
		{"1990-13-40", time.Time{}, true}, // out-of-range month/day
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseDateOfBirth(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("ParseDateOfBirth(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if !c.wantErr && !got.Equal(c.want) {
				t.Errorf("ParseDateOfBirth(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestMapForm_ExampleFixtures(t *testing.T) {
	geo := providers.GeoResult{Longitude: 50.05, Latitude: -5.05}

	cases := []struct {
		file string
		want forms.TransformedForm
	}{
		{
			file: "../../examples/person_one.json",
			want: forms.TransformedForm{
				SessionID:            "c8267b77-d796-451e-9948-e82f56412b56",
				ApplicationReference: "GRU-123089-2026",
				FirstName:            "John",
				LastName:             "Doe",
				Email:                "john.doe@example.com",
				Gender:               "male",
				DateOfBirth:          time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
				PhoneNumber:          strPtr("07123456789"),
				MobileNumber:         "07123456789",
				AddressLine1:         "Stratford Village Surgery",
				AddressLine2:         "50C Romford Road",
				AddressLine3:         strPtr("London"),
				Postcode:             "E15 4BZ",
				Country:              "United Kingdom",
				Longitude:            50.05,
				Latitude:             -5.05,
			},
		},
		{
			// Multi-part name + "other" gender mapping, and phone_number
			// present-but-odd-looking rather than absent.
			file: "../../examples/person_two.json",
			want: forms.TransformedForm{
				SessionID:            "c77fb77f-5a95-4935-9d5a-12953f29da89",
				ApplicationReference: "GRU-123090-2026",
				FirstName:            "Andy",
				LastName:             "James Smith-Jones",
				Email:                "andy.smith.jones@example.com",
				Gender:               "prefer-not-to-say",
				DateOfBirth:          time.Date(1985, 6, 20, 0, 0, 0, 0, time.UTC),
				PhoneNumber:          strPtr("0001"),
				MobileNumber:         "07777777777",
				AddressLine1:         "1 The Avenue",
				AddressLine2:         "Bristol",
				AddressLine3:         nil,
				Postcode:             "BS1 1AA",
				Country:              "United Kingdom",
				Longitude:            50.05,
				Latitude:             -5.05,
			},
		},
		{
			// Omits phone_number and address_line_3 entirely; exercises the
			// *string-is-nil path rather than the fixture-one present path.
			file: "../../examples/person_three.json",
			want: forms.TransformedForm{
				SessionID:            "881fa3b2-84cd-4517-b909-84a073ca0110",
				ApplicationReference: "GRU-123092-2026",
				FirstName:            "Jane",
				LastName:             "Doe",
				Email:                "jane.doe@example.com",
				Gender:               "female",
				DateOfBirth:          time.Date(1921, 3, 14, 0, 0, 0, 0, time.UTC),
				PhoneNumber:          nil,
				MobileNumber:         "07123456789",
				AddressLine1:         "123 Main St",
				AddressLine2:         "Apt 1",
				AddressLine3:         nil,
				Postcode:             "SW1A 1AA",
				Country:              "United Kingdom",
				Longitude:            50.05,
				Latitude:             -5.05,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			data, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var in forms.IngestedForm
			if err := json.Unmarshal(data, &in); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}

			got, err := MapForm(in, geo)
			if err != nil {
				t.Fatalf("MapForm() error = %v", err)
			}

			if got.FirstName != c.want.FirstName || got.LastName != c.want.LastName {
				t.Errorf("name = (%q, %q), want (%q, %q)", got.FirstName, got.LastName, c.want.FirstName, c.want.LastName)
			}
			if got.Gender != c.want.Gender {
				t.Errorf("gender = %q, want %q", got.Gender, c.want.Gender)
			}
			if !got.DateOfBirth.Equal(c.want.DateOfBirth) {
				t.Errorf("dateOfBirth = %v, want %v", got.DateOfBirth, c.want.DateOfBirth)
			}
			if !equalStrPtr(got.PhoneNumber, c.want.PhoneNumber) {
				t.Errorf("phoneNumber = %v, want %v", derefOrNil(got.PhoneNumber), derefOrNil(c.want.PhoneNumber))
			}
			if !equalStrPtr(got.AddressLine3, c.want.AddressLine3) {
				t.Errorf("addressLine3 = %v, want %v", derefOrNil(got.AddressLine3), derefOrNil(c.want.AddressLine3))
			}
			if got.SessionID != c.want.SessionID || got.ApplicationReference != c.want.ApplicationReference ||
				got.Email != c.want.Email || got.MobileNumber != c.want.MobileNumber ||
				got.AddressLine1 != c.want.AddressLine1 || got.AddressLine2 != c.want.AddressLine2 ||
				got.Postcode != c.want.Postcode || got.Country != c.want.Country ||
				got.Longitude != c.want.Longitude || got.Latitude != c.want.Latitude {
				t.Errorf("got = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestMapForm_InvalidGenderAndDateBothReported(t *testing.T) {
	in := forms.IngestedForm{
		Name:        "Test User",
		Gender:      "unknown",
		DateOfBirth: "not-a-date",
	}
	_, err := MapForm(in, providers.GeoResult{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "gender") || !strings.Contains(err.Error(), "date_of_birth") {
		t.Errorf("expected error to mention both gender and date_of_birth, got: %v", err)
	}
}

// validIngestedForm returns a fully populated, schema-conformant form so
// each required-field test can zero out exactly one field.
func validIngestedForm() forms.IngestedForm {
	return forms.IngestedForm{
		SessionID:            "sess-1",
		ApplicationReference: "APP-1",
		Name:                 "Test User",
		Email:                "t@example.com",
		Gender:               "male",
		DateOfBirth:          "1990-01-01",
		MobileNumber:         "0700",
		Address: forms.IngestedAddress{
			AddressLine1: "1 Rd",
			AddressLine2: "Town",
			Postcode:     "AB1 2CD",
			Country:      "UK",
		},
	}
}

func TestValidate_FullyPopulatedForm_NoError(t *testing.T) {
	if err := Validate(validIngestedForm()); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestValidate_SchemaDrift_MissingRequiredField covers the case the mapping
// step used to miss entirely: an upstream schema change that silently drops
// (or blanks) a required field must fail validation rather than flow through
// to a "ready" transformed record with a blank value.
func TestValidate_SchemaDrift_MissingRequiredField(t *testing.T) {
	cases := []struct {
		field  string
		mutate func(*forms.IngestedForm)
	}{
		{"name", func(f *forms.IngestedForm) { f.Name = "" }},
		{"name", func(f *forms.IngestedForm) { f.Name = "   " }}, // whitespace-only counts as blank
		{"email", func(f *forms.IngestedForm) { f.Email = "" }},
		{"mobile_number", func(f *forms.IngestedForm) { f.MobileNumber = "" }},
		{"address.address_line_1", func(f *forms.IngestedForm) { f.Address.AddressLine1 = "" }},
		{"address.address_line_2", func(f *forms.IngestedForm) { f.Address.AddressLine2 = "" }},
		{"address.postcode", func(f *forms.IngestedForm) { f.Address.Postcode = "" }},
		{"address.country", func(f *forms.IngestedForm) { f.Address.Country = "" }},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			in := validIngestedForm()
			c.mutate(&in)

			err := Validate(in)
			if err == nil {
				t.Fatalf("Validate() = nil, want an error for blank %s", c.field)
			}
			if !strings.Contains(err.Error(), c.field) {
				t.Errorf("Validate() error = %v, want it to mention %q", err, c.field)
			}

			if _, mapErr := MapForm(in, providers.GeoResult{}); mapErr == nil {
				t.Errorf("MapForm() = nil error, want it to reject blank %s too", c.field)
			}
		})
	}
}

// address_line_3 and phone_number are declared optional in the ingested
// schema (*string) — blank/absent must not fail validation.
func TestValidate_OptionalFieldsAbsent_NoError(t *testing.T) {
	in := validIngestedForm()
	in.PhoneNumber = nil
	in.Address.AddressLine3 = nil
	if err := Validate(in); err != nil {
		t.Errorf("Validate() = %v, want nil (optional fields must not be required)", err)
	}
}

func equalStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
