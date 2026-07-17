package i18n

import "time"

// Date formats t's date with the locale's DateLayout.
//
// t is rendered in whatever location it carries: this package does not
// convert timezones, because a locale does not have one. Convert at the call
// site with t.In(loc) when you need to.
func (b *Bundle) Date(loc Locale, t time.Time) string {
	return string(b.AppendDate(make([]byte, 0, 32), loc, t))
}

// AppendDate is Date appending into dst.
func (b *Bundle) AppendDate(dst []byte, loc Locale, t time.Time) []byte {
	return t.AppendFormat(dst, b.specFor(b.locIdx(loc)).DateLayout)
}

// Time formats t's clock time with the locale's TimeLayout.
func (b *Bundle) Time(loc Locale, t time.Time) string {
	return string(b.AppendTime(make([]byte, 0, 16), loc, t))
}

// AppendTime is Time appending into dst.
func (b *Bundle) AppendTime(dst []byte, loc Locale, t time.Time) []byte {
	return t.AppendFormat(dst, b.specFor(b.locIdx(loc)).TimeLayout)
}

// DateTime formats t with the locale's DateTimeLayout.
func (b *Bundle) DateTime(loc Locale, t time.Time) string {
	return string(b.AppendDateTime(make([]byte, 0, 40), loc, t))
}

// AppendDateTime is DateTime appending into dst.
func (b *Bundle) AppendDateTime(dst []byte, loc Locale, t time.Time) []byte {
	return t.AppendFormat(dst, b.specFor(b.locIdx(loc)).DateTimeLayout)
}
