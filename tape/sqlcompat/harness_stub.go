//go:build !turso_eval

package sqlcompat

func tursoDriverRegistered() bool { return false }
