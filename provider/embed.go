package provider

// EmbedRequest is the request for embeddings.
type EmbedRequest struct {
	Model string
	Input []string
}

// EmbedData holds a single embedding.
type EmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbedResponse is the response from an embedding request.
type EmbedResponse struct {
	Data []EmbedData `json:"data"`
}
