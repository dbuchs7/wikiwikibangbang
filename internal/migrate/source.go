// Package migrate imports content from Jira and Confluence into items
// (ADR-0023). Readers for each source API or export format produce a
// Snapshot, the canonical source model; Derive proposes a Mapping from it;
// Run writes the result through the validated store commands, in two passes,
// idempotently, optionally as a dry run or as a one-way sync.
//
// The readers for Atlassian Cloud (ConfluenceCloud, JiraCloud) produce a
// Snapshot through the REST APIs; readers for other sources plug into the
// same seam.
package migrate

// Snapshot is the canonical source model of one Atlassian site.
type Snapshot struct {
	// Instance identifies the site, e.g. its host name. Together with the
	// system ("jira", "confluence") and an object's ID it forms the origin.
	Instance string

	Users []SrcUser

	// Jira
	Projects   []SrcSpace
	IssueTypes []SrcIssueType
	Issues     []SrcIssue

	// Confluence
	Spaces []SrcSpace
	Pages  []SrcPage
}

// SrcUser is a source account.
type SrcUser struct {
	ID    string
	Name  string
	Email string
}

// SrcSpace is a Jira project or a Confluence space.
type SrcSpace struct {
	Key  string
	Name string
}

// SrcIssueType is a Jira issue type with its fields and workflow.
type SrcIssueType struct {
	Name     string
	Fields   []SrcField
	Workflow SrcWorkflow
}

// SrcField is a Jira field. Kind is one of "string", "number", "date",
// "user", "option"; anything else cannot be mapped and is reported.
type SrcField struct {
	ID      string
	Name    string
	Kind    string
	Options []string
}

// SrcWorkflow is a Jira workflow.
type SrcWorkflow struct {
	Initial     string
	Statuses    []string
	Transitions []SrcTransition
}

// SrcTransition is a Jira workflow transition.
type SrcTransition struct {
	Name string
	From []string
	To   string
}

// SrcIssue is a Jira issue. Parent is the external ID of the parent issue
// (sub-tasks). Description is plain text; readers convert Jira's rich
// formats before. Raw is the original representation, kept on the item.
type SrcIssue struct {
	ID          string
	Key         string
	Project     string
	Type        string
	Summary     string
	Status      string
	Parent      string
	Description string
	Fields      map[string]any
	Links       []SrcLink
	Version     string
	Raw         string
}

// SrcLink is a Jira issue link to another issue, by external ID.
type SrcLink struct {
	Type   string
	Target string
}

// SrcPage is a Confluence page. Body is in storage format. The allow lists
// are page restrictions, as source account IDs.
type SrcPage struct {
	ID            string
	Space         string
	Title         string
	Parent        string
	Body          string
	ReadAllowList []string
	EditAllowList []string
	Version       string
	Raw           string
}
