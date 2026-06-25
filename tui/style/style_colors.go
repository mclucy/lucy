package style

var (
	Bold      func(any) string
	Dim       func(any) string
	Italic    func(any) string
	Underline func(any) string
	Red       func(any) string
	Green     func(any) string
	Yellow    func(any) string
	Blue      func(any) string
	Magenta   func(any) string
	Cyan      func(any) string

	// Semantic roles — use these instead of raw colors.
	Key     func(any) string // KV field labels (bold magenta)
	Muted   func(any) string // secondary info, annotations (faint)
	Accent  func(any) string // emphasized names, highlights (bold)
	Success func(any) string // checkmarks, completion (green)
	Failure func(any) string // error indicators (red)
	Warning func(any) string // warnings (yellow)
	Link    func(any) string // URLs (underline)
	Note    func(any) string // informational markers (cyan)
)
