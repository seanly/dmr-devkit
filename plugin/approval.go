package plugin

// Decision represents a policy evaluation result.
type Decision struct {
	Action  string         // "allow", "deny", "require_approval"
	Reason  string
	Risk    string         // "low", "medium", "high"
	Details map[string]any // extra decision metadata (e.g. updated_args, context)
}

// ApprovalChoice represents the user's approval decision.
type ApprovalChoice int

const (
	Denied          ApprovalChoice = iota
	ApprovedOnce
	ApprovedSession
	ApprovedAlways
)

// ApprovalRequest is sent to an Approver for human review.
type ApprovalRequest struct {
	Tool     string
	Args     map[string]any
	Decision Decision
	// Tape is an optional routing/context hint for external approvers.
	// It typically equals the tool context's tape name (toolCtx.Tape).
	Tape string
}

// ApprovalResult is the outcome of an approval request.
type ApprovalResult struct {
	Choice  ApprovalChoice
	Comment string
}

// BatchApprovalResult is the outcome of a batch approval request.
// Choice applies to all approved items. Approved is the set of approved indices.
// If Approved is nil, the Choice applies to ALL items.
type BatchApprovalResult struct {
	Choice   ApprovalChoice
	Approved []int  // nil = all, otherwise specific indices (0-based)
	Comment  string // optional comment from user
}

// ApprovalRequiredError is returned by PolicyChecker to signal that an
// approval decision is required before a tool call can proceed.
type ApprovalRequiredError struct {
	Decision Decision
}

func (e *ApprovalRequiredError) Error() string {
	return "approval required: " + e.Decision.Reason
}
