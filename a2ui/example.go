package a2ui

// ExampleSystemPrompt returns a short system prompt fragment that instructs
// the model to use the send_a2ui_json_to_client tool.
func ExampleSystemPrompt() string {
	return `You can render rich UI for the user by calling the send_a2ui_json_to_client tool.
When the user asks for a form, a dashboard, or any structured display, emit A2UI JSON.
Always start with createSurface, then updateComponents (with a root component), then updateDataModel.`
}
