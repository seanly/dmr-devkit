//go:build turso_eval

package sqlcompat

import _ "turso.tech/database/tursogo"

func tursoDriverRegistered() bool { return true }
