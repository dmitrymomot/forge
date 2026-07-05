package config

import (
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
)

// Dotenv loads .env files in order — later files override earlier ones — and
// applies the result to the process environment, but never overwrites a key
// already present in the real environment. So precedence is: process env >
// last file > ... > first file.
func Dotenv(paths ...string) error {
	merged := map[string]string{}
	for _, p := range paths {
		kv, err := parseDotenv(p)
		if err != nil {
			return err
		}
		maps.Copy(merged, kv)
	}
	for k, v := range merged {
		if _, present := os.LookupEnv(k); present {
			continue
		}
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("%w: %v", ErrDotenv, err)
		}
	}
	return nil
}

func parseDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDotenv, err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = unquoteEnv(strings.TrimSpace(v))
	}
	return out, nil
}

func unquoteEnv(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	return v
}
