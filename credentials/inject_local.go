package credentials

import "context"

// InjectLocalBindings parses args["credential_bindings"], resolves them against
// store, and writes _runtime_env / _runtime_inject_tempfiles. No-op when there
// are no bindings.
func InjectLocalBindings(ctx context.Context, store Store, args map[string]any) error {
	if args == nil || store == nil {
		return nil
	}
	raw, ok := args["credential_bindings"]
	if !ok || raw == nil {
		return nil
	}

	bindings, err := ParseBindings(raw)
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return nil
	}

	credEnv, credTemp, err := ResolveBindings(ctx, store, bindings)
	if err != nil {
		return err
	}

	if len(credEnv) > 0 {
		existing := getOrCreateRuntimeEnvMap(args)
		for k, v := range credEnv {
			existing[k] = v
		}
		args[RuntimeEnvKey] = existing
	}
	if len(credTemp) > 0 {
		appendRuntimeTempFiles(args, credTemp)
	}
	return nil
}
