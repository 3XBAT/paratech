package values

import "strings"

// HomeworkMarkdown renders the exported homework file of an assignment: the title as a heading, a blank line, the
// body and a final line break. The draft export and the export proposal both write the file a homework email
// attaches, so they share this one format.
func HomeworkMarkdown(title, body string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + strings.TrimSpace(body) + "\n"
}
