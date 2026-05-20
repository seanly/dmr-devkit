package a2aserver

import (
	"encoding/json"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// MetadataKeyDMRContext is the A2A Message.metadata key for DMR caller context (map or JSON string).
// Inbound servers pass this to [Runner.Run] as contextJSON; outbound clients should set it via [AttachContextMetadata].
const MetadataKeyDMRContext = "dmr_context"

// ContextJSONFromMessage extracts caller context from an inbound A2A user message.
func ContextJSONFromMessage(m *a2a.Message) string {
	if m == nil || m.Metadata == nil {
		return ""
	}
	return metadataToContextJSON(m.Metadata[MetadataKeyDMRContext])
}

// AttachContextMetadata sets Message.metadata[MetadataKeyDMRContext] for the remote agent.
func AttachContextMetadata(msg *a2a.Message, ctx map[string]any) {
	if msg == nil || len(ctx) == 0 {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata[MetadataKeyDMRContext] = ctx
}

func metadataToContextJSON(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case map[string]any:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return string(b)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
