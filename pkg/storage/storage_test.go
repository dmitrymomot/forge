package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_applyDefaults(t *testing.T) {
	t.Parallel()

	t.Run("empty config gets defaults", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		cfg.applyDefaults()

		require.Equal(t, DefaultRegion, cfg.Region)
		require.Equal(t, ACLPrivate, cfg.DefaultACL)
		require.Equal(t, int64(DefaultMaxDownloadSize), cfg.MaxDownloadSize)
	})

	t.Run("existing values preserved", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Region:          "eu-west-1",
			DefaultACL:      ACLPublicRead,
			MaxDownloadSize: 100 << 20,
		}
		cfg.applyDefaults()

		require.Equal(t, "eu-west-1", cfg.Region)
		require.Equal(t, ACLPublicRead, cfg.DefaultACL)
		require.Equal(t, int64(100<<20), cfg.MaxDownloadSize)
	})
}

func TestConfig_validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Bucket:    "my-bucket",
				AccessKey: "access-key",
				SecretKey: "secret-key",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			cfg: Config{
				AccessKey: "access-key",
				SecretKey: "secret-key",
			},
			wantErr: true,
		},
		{
			name: "missing access key",
			cfg: Config{
				Bucket:    "my-bucket",
				SecretKey: "secret-key",
			},
			wantErr: true,
		},
		{
			name: "missing secret key",
			cfg: Config{
				Bucket:    "my-bucket",
				AccessKey: "access-key",
			},
			wantErr: true,
		},
		{
			name:    "empty config",
			cfg:     Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidConfig)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestACLConstants(t *testing.T) {
	t.Parallel()

	require.Equal(t, ACL("private"), ACLPrivate)
	require.Equal(t, ACL("public-read"), ACLPublicRead)
}

func TestDefaultConstants(t *testing.T) {
	t.Parallel()

	require.Equal(t, "us-east-1", DefaultRegion)
	require.Equal(t, int64(50<<20), int64(DefaultMaxDownloadSize))
	require.Equal(t, 15*60, DefaultSignedURLExpiry)
}
