package i18n

import "errors"

var (
	ErrEmptyLanguage  = errors.New("i18n: language cannot be empty")
	ErrEmptyNamespace = errors.New("i18n: namespace cannot be empty")
	ErrNilPluralRule  = errors.New("i18n: plural rule cannot be nil")
	ErrInvalidFile    = errors.New("i18n: invalid translation file")
)
