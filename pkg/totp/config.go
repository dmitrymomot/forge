package totp

type Config struct {
	EncryptionKey string `env:"ENCRYPTION_KEY,required"`
}
