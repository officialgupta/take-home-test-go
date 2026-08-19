package forms

import "time"

// TransformedForm mirrors reference-ts/src/forms/schemas/transformed_schema.ts.
type TransformedForm struct {
	SessionID            string    `json:"sessionId"`
	ApplicationReference string    `json:"applicationReference"`
	FirstName            string    `json:"firstName"`
	LastName             string    `json:"lastName"`
	Email                string    `json:"email"`
	Gender               string    `json:"gender"`
	DateOfBirth          time.Time `json:"dateOfBirth"`
	PhoneNumber          *string   `json:"phoneNumber,omitempty"`
	MobileNumber         string    `json:"mobileNumber"`
	AddressLine1         string    `json:"addressLine1"`
	AddressLine2         string    `json:"addressLine2"`
	AddressLine3         *string   `json:"addressLine3,omitempty"`
	Postcode             string    `json:"postcode"`
	Country              string    `json:"country"`
	Longitude            float64   `json:"longitude"`
	Latitude             float64   `json:"latitude"`
}
