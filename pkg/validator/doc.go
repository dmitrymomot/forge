// Package validator provides a rule-based data validation system with both
// programmatic and struct tag-based validation capabilities.
//
// The package is built around a Rule type that encapsulates validation logic
// and error reporting, allowing for flexible and composable validation patterns.
//
// # Core Concepts
//
// Rules are created by validation functions that return a Rule struct containing
// both the validation check and error details:
//
//	import "github.com/dmitrymomot/forge/pkg/validator"
//
//	// Create validation rules
//	rules := []validator.Rule{
//		validator.Required("username", user.Username),
//		validator.MinLen("username", user.Username, 3),
//		validator.ValidEmail("email", user.Email),
//	}
//
//	// Apply all rules
//	if err := validator.Apply(rules...); err != nil {
//		// Handle validation errors
//		validationErrs := validator.ExtractValidationErrors(err)
//		for _, verr := range validationErrs {
//			fmt.Printf("Field: %s, Message: %s\n", verr.Field, verr.Message)
//		}
//	}
//
// # String Validation
//
// String validation functions for common requirements:
//
//	rules := []validator.Rule{
//		validator.Required("name", user.Name),
//		validator.MinLen("name", user.Name, 2),
//		validator.MaxLen("name", user.Name, 50),
//		validator.ValidEmail("email", user.Email),
//		validator.ValidURL("website", user.Website),
//		validator.ValidPhone("phone", user.Phone),
//		validator.ValidAlphanumeric("username", user.Username),
//	}
//
// # Numeric Validation
//
// Type-safe numeric validation using generics:
//
//	rules := []validator.Rule{
//		validator.Min("age", user.Age, 18),
//		validator.Max("age", user.Age, 120),
//		validator.Min("price", product.Price, 0.01),
//		validator.Max("quantity", order.Quantity, 1000),
//	}
//
// # Collection Validation
//
// Validation for slices and maps:
//
//	rules := []validator.Rule{
//		validator.RequiredSlice("items", order.Items),
//		validator.MinLenSlice("items", order.Items, 1),
//		validator.MaxLenSlice("tags", post.Tags, 10),
//		validator.RequiredMap("metadata", user.Metadata),
//	}
//
// # Choice Validation
//
// Validate values against allowed/forbidden lists:
//
//	validStatuses := []string{"active", "inactive", "pending"}
//	rules := []validator.Rule{
//		validator.InList("status", user.Status, validStatuses),
//	}
//
//	forbiddenUsernames := []string{"admin", "root", "system"}
//	rules = append(rules,
//		validator.NotInList("username", user.Username, forbiddenUsernames),
//	)
//
// # UUID Validation
//
// UUID validation with version support:
//
//	import "github.com/google/uuid"
//
//	rules := []validator.Rule{
//		validator.ValidUUID("id", user.ID),
//		validator.ValidUUIDv4String("session_id", session.ID),
//		validator.RequiredUUID("user_id", userUUID),
//	}
//
// # Format Validation
//
// Various format validators:
//
//	rules := []validator.Rule{
//		validator.ValidEmail("email", user.Email),
//		validator.ValidURL("website", user.Website),
//		validator.ValidURLWithScheme("api_url", config.APIURL, []string{"https"}),
//		validator.ValidPhone("phone", user.Phone),
//		validator.ValidIPv4("server", config.ServerIP),
//		validator.ValidMAC("device_mac", device.MACAddress),
//	}
//
// # Financial Validation
//
// Financial data validation:
//
//	rules := []validator.Rule{
//		validator.PositiveAmount("amount", payment.Amount),
//		validator.ValidCurrencyCode("currency", payment.Currency),
//		validator.ValidCreditCardChecksum("card_number", payment.CardNumber),
//		validator.DecimalPrecision("price", product.Price, 2),
//		validator.ValidPercentage("tax_rate", order.TaxRate),
//	}
//
// # Pattern Validation
//
// Regular expression and pattern matching:
//
//	rules := []validator.Rule{
//		validator.MatchesRegex("sku", product.SKU, `^[A-Z]{2}\d{4}$`, "product SKU"),
//		validator.NoWhitespace("api_key", config.APIKey),
//		validator.PrintableChars("description", item.Description),
//	}
//
// # Struct Tag Validation
//
// The package supports struct tag-based validation:
//
//	type User struct {
//		Username string `validate:"required;min:3;max:20;alphanum"`
//		Email    string `validate:"required;email"`
//		Age      int    `validate:"min:18;max:120"`
//		Status   string `validate:"in:active,inactive,pending"`
//	}
//
//	user := User{
//		Username: "john_doe",
//		Email:    "john@example.com",
//		Age:      25,
//		Status:   "active",
//	}
//
//	if err := validator.ValidateStruct(&user); err != nil {
//		// Handle validation errors
//		validationErrs := validator.ExtractValidationErrors(err)
//		for _, verr := range validationErrs {
//			fmt.Printf("%s: %s\n", verr.Field, verr.Message)
//		}
//	}
//
// # Custom Validators
//
// Register custom validation functions for struct tags:
//
//	validator.RegisterValidator("business_email", func(field string, value reflect.Value, params []string) validator.Rule {
//		email := value.String()
//		return validator.Rule{
//			Check: func() bool {
//				// Custom business email validation logic
//				return strings.HasSuffix(email, "@company.com")
//			},
//			Error: validator.ValidationError{
//				Field:   field,
//				Message: "must be a company email address",
//			},
//		}
//	})
//
// # Error Handling
//
// ValidationErrors provides methods for working with validation results:
//
//	if err := validator.Apply(rules...); err != nil {
//		if validator.IsValidationError(err) {
//			validationErrs := validator.ExtractValidationErrors(err)
//
//			// Check for specific field errors
//			if validationErrs.Has("email") {
//				emailErrors := validationErrs.Get("email")
//				fmt.Println("Email errors:", emailErrors)
//			}
//
//			// Get all failed fields
//			failedFields := validationErrs.Fields()
//			fmt.Println("Failed fields:", failedFields)
//		}
//	}
//
// # Translation Support
//
// ValidationError includes translation keys and values for internationalization:
//
//	for _, verr := range validationErrs {
//		// Use translation key and values with your i18n system
//		translatedMsg := translator.Translate(verr.TranslationKey, verr.TranslationValues)
//		fmt.Printf("%s: %s\n", verr.Field, translatedMsg)
//	}
package validator
