package i18n_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestLocaleFormat_FormatNumber(t *testing.T) {
	t.Parallel()

	t.Run("English format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnUS()

		require.Equal(t, "1,234", lf.FormatNumber(1234))
		require.Equal(t, "1,234.5", lf.FormatNumber(1234.5))
		require.Equal(t, "1,234,567.89", lf.FormatNumber(1234567.89))
		require.Equal(t, "-1,234.5", lf.FormatNumber(-1234.5))
		require.Equal(t, "123", lf.FormatNumber(123))
		require.Equal(t, "0", lf.FormatNumber(0))
	})

	t.Run("European format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.NewLocaleFormat(
			i18n.WithDecimalSeparator(","),
			i18n.WithThousandSeparator("."),
		)

		require.Equal(t, "1.234", lf.FormatNumber(1234))
		require.Equal(t, "1.234,5", lf.FormatNumber(1234.5))
		require.Equal(t, "1.234.567,89", lf.FormatNumber(1234567.89))
		require.Equal(t, "-1.234,5", lf.FormatNumber(-1234.5))
	})

	t.Run("space as thousand separator", func(t *testing.T) {
		t.Parallel()
		lf := i18n.NewLocaleFormat(
			i18n.WithDecimalSeparator(","),
			i18n.WithThousandSeparator(" "),
		)

		require.Equal(t, "1 234", lf.FormatNumber(1234))
		require.Equal(t, "1 234,5", lf.FormatNumber(1234.5))
		require.Equal(t, "1 234 567,89", lf.FormatNumber(1234567.89))
	})
}

func TestLocaleFormat_FormatCurrency(t *testing.T) {
	t.Parallel()

	t.Run("English/USD format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnUS()

		require.Equal(t, "$1,234.50", lf.FormatCurrency(1234.50))
		require.Equal(t, "$1,234.00", lf.FormatCurrency(1234))
		require.Equal(t, "-$1,234.50", lf.FormatCurrency(-1234.50))
		require.Equal(t, "$0.99", lf.FormatCurrency(0.99))
		require.Equal(t, "$0.00", lf.FormatCurrency(0))
	})

	t.Run("Euro format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatDeDE()

		require.Equal(t, "1.234,50 \u20ac", lf.FormatCurrency(1234.50))
		require.Equal(t, "1.234,00 \u20ac", lf.FormatCurrency(1234))
		require.Equal(t, "-1.234,50 \u20ac", lf.FormatCurrency(-1234.50))
	})

	t.Run("Pound format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnGB()

		require.Equal(t, "\u00a31,234.50", lf.FormatCurrency(1234.50))
	})

	t.Run("Brazilian Real format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatPtBR()

		require.Equal(t, "R$1.234,50", lf.FormatCurrency(1234.50))
	})
}

func TestLocaleFormat_FormatPercent(t *testing.T) {
	t.Parallel()

	t.Run("English format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnUS()

		require.Equal(t, "50%", lf.FormatPercent(0.5))
		require.Equal(t, "100%", lf.FormatPercent(1.0))
		require.Equal(t, "0%", lf.FormatPercent(0))
		require.Equal(t, "25.5%", lf.FormatPercent(0.255))
		require.Equal(t, "-15%", lf.FormatPercent(-0.15))
		require.Equal(t, "0.5%", lf.FormatPercent(0.005))
	})

	t.Run("comma decimal format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.NewLocaleFormat(
			i18n.WithDecimalSeparator(","),
		)

		require.Equal(t, "50%", lf.FormatPercent(0.5))
		require.Equal(t, "25,5%", lf.FormatPercent(0.255))
		require.Equal(t, "-15%", lf.FormatPercent(-0.15))
	})
}

func TestLocaleFormat_FormatDate(t *testing.T) {
	t.Parallel()

	testDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	t.Run("US format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnUS()
		require.Equal(t, "01/02/2024", lf.FormatDate(testDate))
	})

	t.Run("German format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatDeDE()
		require.Equal(t, "02.01.2024", lf.FormatDate(testDate))
	})

	t.Run("Chinese format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatZhCN()
		require.Equal(t, "2024-01-02", lf.FormatDate(testDate))
	})
}

func TestLocaleFormat_FormatTime(t *testing.T) {
	t.Parallel()

	testTime := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)

	t.Run("US 12-hour format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnUS()
		require.Equal(t, "3:04 PM", lf.FormatTime(testTime))
	})

	t.Run("German 24-hour format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatDeDE()
		require.Equal(t, "15:04", lf.FormatTime(testTime))
	})
}

func TestLocaleFormat_FormatDateTime(t *testing.T) {
	t.Parallel()

	testDateTime := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)

	t.Run("US format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatEnUS()
		require.Equal(t, "01/02/2024 3:04 PM", lf.FormatDateTime(testDateTime))
	})

	t.Run("German format", func(t *testing.T) {
		t.Parallel()
		lf := i18n.FormatDeDE()
		require.Equal(t, "02.01.2024 15:04", lf.FormatDateTime(testDateTime))
	})
}
