package forms

// IngestedAddress mirrors reference-ts/src/forms/schemas/ingested_schema.ts's
// nested address object.
type IngestedAddress struct {
	AddressLine1 string  `json:"address_line_1"`
	AddressLine2 string  `json:"address_line_2"`
	AddressLine3 *string `json:"address_line_3,omitempty"`
	Postcode     string  `json:"postcode"`
	Country      string  `json:"country"`
}

// IngestedForm mirrors reference-ts/src/forms/schemas/ingested_schema.ts.
type IngestedForm struct {
	SessionID            string          `json:"session_id"`
	ApplicationReference string          `json:"application_reference"`
	Name                 string          `json:"name"`
	Email                string          `json:"email"`
	Gender               string          `json:"gender"`
	DateOfBirth          string          `json:"date_of_birth"`
	PhoneNumber          *string         `json:"phone_number,omitempty"`
	MobileNumber         string          `json:"mobile_number"`
	Address              IngestedAddress `json:"address"`
}
