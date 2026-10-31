package main

import "strings"

// Keep shortcuts on a separate screen so the meeting stays easy to scan.
func (m ui) helpView() string {
	var b strings.Builder
	writeAll(&b, m.accent.Bold(true).Render("meetlet"), m.muted.Render("  /  help"), "\n\n")
	b.WriteString(`d / t       Date / time
n / l       Title / length
↑ / ↓       Choose a location (Ctrl-P / Ctrl-N)
← / →       Move a day (Ctrl-B / Ctrl-F)
[ / ]       Move 15 minutes
a / x       Add / remove a location
s / Enter   Use the selected location as the source
e / D       Export / delete a calendar invite (.ics)
q           Quit

Calendar: ←/→ day · ↑/↓ week · PgUp/PgDn month · Home today
Editors: Enter saves · Esc cancels

`)
	writeAll(&b, m.muted.Render(startupJoke), "\n\n", m.muted.Render("Esc / Enter / ? back to meeting"))
	return b.String()
}
