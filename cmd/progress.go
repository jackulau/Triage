package cmd

import (
	"fmt"
	"io"
	"strings"
)

// progressBar is a simple terminal progress bar that writes to stderr.
type progressBar struct {
	total       int
	current     int
	width       int
	description string
	writer      io.Writer
}

// newProgressBar creates a new progress bar.
func newProgressBar(total int, description string, writer io.Writer) *progressBar {
	return &progressBar{
		total:       total,
		width:       30,
		description: description,
		writer:      writer,
	}
}

// Add increments the progress bar by n.
func (p *progressBar) Add(n int) {
	p.current += n
	if p.current > p.total {
		p.current = p.total
	}
	p.render()
}

// Finish completes the progress bar and prints a newline.
func (p *progressBar) Finish() {
	p.current = p.total
	p.render()
	fmt.Fprintln(p.writer)
}

// render draws the progress bar to the writer using carriage return.
func (p *progressBar) render() {
	if p.total <= 0 {
		return
	}

	pct := float64(p.current) / float64(p.total)
	filled := int(pct * float64(p.width))
	if filled > p.width {
		filled = p.width
	}

	bar := strings.Repeat("=", filled) + strings.Repeat(" ", p.width-filled)
	fmt.Fprintf(p.writer, "\r%s [%s] %d/%d", p.description, bar, p.current, p.total)
}
