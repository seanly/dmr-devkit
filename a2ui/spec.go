// Package a2ui implements the A2UI (Agent-to-User Interface) protocol for DMR devkit.
// It provides declarative UI message types, catalog management, prompt generation,
// and a tool that allows LLMs to emit structured UI payloads.
//
// A2UI is an open standard (https://a2ui.org) that lets agents "speak UI" by sending
// JSON payloads describing component trees. Clients render them using native widgets.
package a2ui

// Version is the A2UI specification version supported by this package.
const Version = "v0.10"

// Message is the top-level A2UI message. Exactly one of the payload fields is non-nil.
type Message struct {
	Version          string                `json:"version"`
	CreateSurface    *CreateSurface        `json:"createSurface,omitempty"`
	UpdateComponents *UpdateComponents     `json:"updateComponents,omitempty"`
	UpdateDataModel  *UpdateDataModel      `json:"updateDataModel,omitempty"`
	DeleteSurface    *DeleteSurface        `json:"deleteSurface,omitempty"`
	CallFunction     *CallFunction         `json:"callFunction,omitempty"`
	ActionResponse   *ActionResponse       `json:"actionResponse,omitempty"`
}

// CreateSurface signals the client to create a new UI surface.
type CreateSurface struct {
	SurfaceID     string         `json:"surfaceId"`
	CatalogID     string         `json:"catalogId"`
	Theme         map[string]any `json:"theme,omitempty"`
	SendDataModel bool           `json:"sendDataModel,omitempty"`
}

// UpdateComponents updates a surface with a new component tree.
// One component MUST have id="root".
type UpdateComponents struct {
	SurfaceID  string      `json:"surfaceId"`
	Components []Component `json:"components"`
}

// UpdateDataModel updates the data model for an existing surface.
type UpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path,omitempty"`
	Value     any    `json:"value,omitempty"`
}

// DeleteSurface signals the client to delete a surface.
type DeleteSurface struct {
	SurfaceID string `json:"surfaceId"`
}

// CallFunction is a server-initiated function call to the client.
type CallFunction struct {
	FunctionCallID string         `json:"functionCallId"`
	WantResponse   bool           `json:"wantResponse,omitempty"`
	CallFunction   *FunctionCall  `json:"callFunction,omitempty"`
}

// ActionResponse is a server response to a client action.
type ActionResponse struct {
	ActionID string `json:"actionId"`
	Value    any    `json:"value,omitempty"`
}

// Component is a single UI component in the A2UI catalog.
// The Component field is the discriminator (e.g. "Text", "Button", "Card").
type Component struct {
	ID            string         `json:"id"`
	Component     string         `json:"component"`
	Accessibility *Accessibility `json:"accessibility,omitempty"`

	// Layout / children (used by Row, Column, List, Card, Tabs, Modal, Button)
	Children  any    `json:"children,omitempty"`  // []string | ChildTemplate
	Child     string `json:"child,omitempty"`     // single child component id
	Justify   string `json:"justify,omitempty"`   // Row: start, center, end, spaceBetween, spaceAround, spaceEvenly, stretch
	Align     string `json:"align,omitempty"`     // Row/Column: start, center, end, stretch
	Weight    int    `json:"weight,omitempty"`    // flex weight within Row/Column
	Axis      string `json:"axis,omitempty"`      // Divider: horizontal | vertical

	// Content (used by Text, Image, Icon, Video, AudioPlayer)
	Text        any    `json:"text,omitempty"`        // string | DataBinding | FunctionCall
	URL         any    `json:"url,omitempty"`         // string | DataBinding | FunctionCall
	PosterURL   any    `json:"posterUrl,omitempty"`   // string | DataBinding | FunctionCall
	Description any    `json:"description,omitempty"` // string | DataBinding | FunctionCall
	Name        any    `json:"name,omitempty"`        // Icon name: string | DataBinding
	Fit         string `json:"fit,omitempty"`         // Image: contain, cover, fill, none, scaleDown
	Variant     string `json:"variant,omitempty"`     // Text: h1..h5, caption, body; Image: icon, avatar, smallFeature...

	// Input (used by TextField, CheckBox, ChoicePicker, Slider, DateTimeInput)
	Label       any    `json:"label,omitempty"`       // string | DataBinding | FunctionCall
	Hint        any    `json:"hint,omitempty"`        // string | DataBinding | FunctionCall
	Value       any    `json:"value,omitempty"`       // any | DataBinding
	Options     any    `json:"options,omitempty"`     // []Option | DataBinding
	Min         any    `json:"min,omitempty"`         // number | DataBinding
	Max         any    `json:"max,omitempty"`         // number | DataBinding
	Step        any    `json:"step,omitempty"`        // number | DataBinding
	Mode        string `json:"mode,omitempty"`        // DateTimeInput: date, time, datetime
	Format      string `json:"format,omitempty"`      // DateTimeInput format hint
	MultiSelect bool   `json:"multiSelect,omitempty"` // ChoicePicker
	Required    bool   `json:"required,omitempty"`    // TextField

	// Interaction (used by Button)
	Action *Action `json:"action,omitempty"`

	// Validation
	Checks []CheckRule `json:"checks,omitempty"`

	// Card-specific
	Header  string `json:"header,omitempty"`  // Card header component id
	Footer  string `json:"footer,omitempty"`  // Card footer component id

	// Tabs-specific
	Tabs []TabItem `json:"tabs,omitempty"`

	// Modal-specific
	Trigger string `json:"trigger,omitempty"` // Modal trigger component id
	Content string `json:"content,omitempty"` // Modal content component id

	// Table-specific
	Columns []TableColumn `json:"columns,omitempty"`
	Rows    any           `json:"rows,omitempty"` // DataBinding | []map[string]any

	// Chart-specific
	ChartType string `json:"chartType,omitempty"` // bar, line, pie, scatter
	XAxis     string `json:"xAxis,omitempty"`     // data model path
	YAxis     string `json:"yAxis,omitempty"`     // data model path
	Series    any    `json:"series,omitempty"`    // DataBinding | []ChartSeries

	// Progress
	Progress any `json:"progress,omitempty"` // number | DataBinding | FunctionCall
}

// Accessibility attributes for assistive technologies.
type Accessibility struct {
	Label       any `json:"label,omitempty"`       // string | DataBinding | FunctionCall
	Description any `json:"description,omitempty"` // string | DataBinding | FunctionCall
}

// ChildList is either a static list of component IDs or a dynamic template.
// In JSON it is represented directly: []string or {"componentId":"...","path":"..."}.

type ChildTemplate struct {
	ComponentID string `json:"componentId"`
	Path        string `json:"path"`
}

// DataBinding references a value in the data model by JSON Pointer path.
type DataBinding struct {
	Path string `json:"path"`
}

// FunctionCall invokes a named function on the client.
type FunctionCall struct {
	Call         string         `json:"call"`
	Args         map[string]any `json:"args,omitempty"`
	ReturnType   string         `json:"returnType,omitempty"`   // array, boolean, number, object, string, void
	CallableFrom string         `json:"callableFrom,omitempty"` // clientOnly, remoteOnly, clientOrRemote
}

// Action defines an interaction handler.
type Action struct {
	Event        *ActionEvent  `json:"event,omitempty"`
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`
}

// ActionEvent triggers a server-side event.
type ActionEvent struct {
	Name         string         `json:"name"`
	Context      map[string]any `json:"context,omitempty"`
	WantResponse bool           `json:"wantResponse,omitempty"`
	ResponsePath string         `json:"responsePath,omitempty"`
}

// CheckRule is a validation rule for input components.
type CheckRule struct {
	Condition any    `json:"condition"` // DynamicBoolean
	Message   string `json:"message"`
}

// Option is a choice option for ChoicePicker, CheckBox, etc.
type Option struct {
	Label any    `json:"label"` // string | DataBinding | FunctionCall
	Value string `json:"value"`
}

// TabItem is a single tab in a Tabs component.
type TabItem struct {
	Title string `json:"title"` // string | DataBinding | FunctionCall
	Child string `json:"child"` // component id
}

// TableColumn defines a column in a DataTable.
type TableColumn struct {
	Label any    `json:"label"` // string | DataBinding | FunctionCall
	Key   string `json:"key"`
}

// ChartSeries defines a data series in a Chart.
type ChartSeries struct {
	Name string `json:"name"`
	Data any    `json:"data"` // DataBinding | []any
}
