package i18n_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestPredefinedFormats(t *testing.T) {
	t.Parallel()

	testDate := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)

	t.Run("FormatEnUS", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatEnUS()
		require.Equal(t, "$1,234.50", f.FormatCurrency(1234.50))
		require.Equal(t, "03/15/2024", f.FormatDate(testDate))
		require.Equal(t, "2:30 PM", f.FormatTime(testDate))
	})

	t.Run("FormatEnGB", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatEnGB()
		require.Equal(t, "\u00a31,234.50", f.FormatCurrency(1234.50))
		require.Equal(t, "15/03/2024", f.FormatDate(testDate))
		require.Equal(t, "14:30", f.FormatTime(testDate))
	})

	t.Run("FormatDeDE", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatDeDE()
		require.Equal(t, "1.234,50 \u20ac", f.FormatCurrency(1234.50))
		require.Equal(t, "15.03.2024", f.FormatDate(testDate))
		require.Equal(t, "14:30", f.FormatTime(testDate))
	})

	t.Run("FormatFrFR", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatFrFR()
		require.Equal(t, "1 234,50 \u20ac", f.FormatCurrency(1234.50))
		require.Equal(t, "15/03/2024", f.FormatDate(testDate))
		require.Equal(t, "14:30", f.FormatTime(testDate))
	})

	t.Run("FormatEsES", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatEsES()
		require.Equal(t, "1.234,50 \u20ac", f.FormatCurrency(1234.50))
		require.Equal(t, "15/03/2024", f.FormatDate(testDate))
	})

	t.Run("FormatPtBR", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatPtBR()
		require.Equal(t, "R$1.234,50", f.FormatCurrency(1234.50))
		require.Equal(t, "15/03/2024", f.FormatDate(testDate))
	})

	t.Run("FormatJaJP", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatJaJP()
		require.Equal(t, "\u00a51,234.50", f.FormatCurrency(1234.50))
		require.Equal(t, "2024/03/15", f.FormatDate(testDate))
		require.Equal(t, "14:30", f.FormatTime(testDate))
	})

	t.Run("FormatZhCN", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatZhCN()
		require.Equal(t, "\u00a51,234.50", f.FormatCurrency(1234.50))
		require.Equal(t, "2024-03-15", f.FormatDate(testDate))
	})

	t.Run("FormatKoKR", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatKoKR()
		require.Equal(t, "\u20a91,234.50", f.FormatCurrency(1234.50))
		require.Equal(t, "2024.03.15", f.FormatDate(testDate))
	})

	t.Run("FormatPlPL", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatPlPL()
		require.Equal(t, "1 234,50 z\u0142", f.FormatCurrency(1234.50))
		require.Equal(t, "15.03.2024", f.FormatDate(testDate))
	})

	t.Run("FormatRuRU", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatRuRU()
		require.Equal(t, "1 234,50 \u20bd", f.FormatCurrency(1234.50))
		require.Equal(t, "15.03.2024", f.FormatDate(testDate))
	})

	t.Run("FormatArSA", func(t *testing.T) {
		t.Parallel()
		f := i18n.FormatArSA()
		require.Equal(t, "1,234.50 SAR", f.FormatCurrency(1234.50))
		require.Equal(t, "15/03/2024", f.FormatDate(testDate))
		require.Equal(t, "2:30 PM", f.FormatTime(testDate))
	})
}
