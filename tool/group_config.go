package tool

// ResolveToolGroup reads the optional "tool_group" key from plugin config
// and returns the corresponding ToolGroup, falling back to defaultGroup.
// Valid values: "core", "extended".
func ResolveToolGroup(config map[string]any, defaultGroup ToolGroup) ToolGroup {
	if config == nil {
		return defaultGroup
	}
	v, ok := config["tool_group"].(string)
	if !ok {
		return defaultGroup
	}
	switch v {
	case "core":
		return ToolGroupCore
	case "extended":
		return ToolGroupExtended
	default:
		return defaultGroup
	}
}
