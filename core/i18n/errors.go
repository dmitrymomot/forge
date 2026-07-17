package i18n

import "errors"

// Sentinel errors. All are errors.Is-matchable and single-line.
var (
	// ErrInvalidConfig reports a Config that failed Validate.
	ErrInvalidConfig = errors.New("i18n: invalid config")
	// ErrInvalidCatalog reports a catalog that could not be loaded:
	// unparseable JSON, or a directory name that is not a language tag.
	ErrInvalidCatalog = errors.New("i18n: invalid catalog")
	// ErrDuplicateKey reports the same key defined twice within one locale.
	ErrDuplicateKey = errors.New("i18n: duplicate key")
	// ErrUnknownLocale reports a tag the bundle's catalogs do not support.
	ErrUnknownLocale = errors.New("i18n: unknown locale")
	// ErrUnknownKey reports a Key absent from the default locale's catalog.
	ErrUnknownKey = errors.New("i18n: unknown key")
	// ErrNilRule reports a nil PluralRule passed to WithPlural.
	ErrNilRule = errors.New("i18n: nil plural rule")
)
