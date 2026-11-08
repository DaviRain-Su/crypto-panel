// Package pages is the declarative page model plugins draw with: a page is
// data, validated here and rendered by native UI, so a plugin can never
// imitate a system dialog or an approval prompt (design §7).
package pages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Page is one screen a plugin returns.
type Page struct {
	Title    string    `json:"title,omitempty"`
	Subtitle string    `json:"subtitle,omitempty"`
	Sections []Section `json:"sections"`
}

// Section is one block of a page. A section may hold one of each block,
// drawn in the order stats, table, cards, links, text, divider, chart.
type Section struct {
	Title   string `json:"title,omitempty"`
	Stats   []Stat `json:"stats,omitempty"`
	Table   *Table `json:"table,omitempty"`
	Cards   []Card `json:"cards,omitempty"`
	Links   []Link `json:"links,omitempty"`
	Text    string `json:"text,omitempty"`
	Divider bool   `json:"divider,omitempty"`
	Chart   *Chart `json:"chart,omitempty"`
}

// Stat is a labelled number.
type Stat struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Table is a grid of plain text.
type Table struct {
	Columns []Column   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Column is one table column; Align is "start" (the default) or "end".
type Column struct {
	Title string `json:"title"`
	Align string `json:"align,omitempty"`
}

// Card is a titled block of text.
type Card struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
}

// Link opens another page of the same plugin.
type Link struct {
	Title string `json:"title"`
	Page  string `json:"page"`
}

// Chart is a line or bar chart of at most 60 points.
type Chart struct {
	Kind   string  `json:"kind"`
	Points []Point `json:"points"`
}

// Point is one chart value.
type Point struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

const (
	maxPageBytes = 256 << 10
	maxSections  = 32
	maxStats     = 12
	maxColumns   = 8
	maxRows      = 200
	maxCards     = 32
	maxLinks     = 16
	maxPoints    = 60
)

var linkPage = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Decode parses and validates a page a plugin returned.
func Decode(data []byte) (Page, error) {
	if len(data) > maxPageBytes {
		return Page{}, fmt.Errorf("page is larger than %d bytes", maxPageBytes)
	}
	var p Page
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Page{}, fmt.Errorf("page: %w", err)
	}
	if dec.More() {
		return Page{}, errors.New("page: trailing data")
	}
	if err := p.Validate(); err != nil {
		return Page{}, err
	}
	return p, nil
}

// DecodeValue marshals v and decodes it as a page, for host.render.
func DecodeValue(v any) (Page, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Page{}, fmt.Errorf("page: %w", err)
	}
	return Decode(data)
}

// Validate checks the page's shape and text. Every string is plain text;
// control characters that could reorder or hide it are refused.
func (p Page) Validate() error {
	if err := checkText("title", p.Title, 120); err != nil {
		return err
	}
	if err := checkText("subtitle", p.Subtitle, 160); err != nil {
		return err
	}
	if len(p.Sections) > maxSections {
		return fmt.Errorf("page: more than %d sections", maxSections)
	}
	for i, s := range p.Sections {
		if err := s.validate(); err != nil {
			return fmt.Errorf("page: section %d: %w", i+1, err)
		}
	}
	return nil
}

func (s Section) validate() error {
	if err := checkText("title", s.Title, 120); err != nil {
		return err
	}
	if len(s.Stats) > maxStats {
		return fmt.Errorf("more than %d stats", maxStats)
	}
	for _, st := range s.Stats {
		if err := checkText("stat label", st.Label, 40); err != nil {
			return err
		}
		if err := checkText("stat value", st.Value, 80); err != nil {
			return err
		}
	}
	if s.Table != nil {
		if err := s.Table.validate(); err != nil {
			return err
		}
	}
	if len(s.Cards) > maxCards {
		return fmt.Errorf("more than %d cards", maxCards)
	}
	for _, card := range s.Cards {
		if err := checkText("card title", card.Title, 80); err != nil {
			return err
		}
		if err := checkText("card subtitle", card.Subtitle, 120); err != nil {
			return err
		}
		if err := checkText("card body", card.Body, 500); err != nil {
			return err
		}
	}
	if len(s.Links) > maxLinks {
		return fmt.Errorf("more than %d links", maxLinks)
	}
	for _, link := range s.Links {
		if err := checkText("link title", link.Title, 60); err != nil {
			return err
		}
		if !linkPage.MatchString(link.Page) {
			return fmt.Errorf("link page %q must be lowercase letters, digits and dashes", link.Page)
		}
	}
	if err := checkText("text", s.Text, 2000); err != nil {
		return err
	}
	if s.Chart != nil {
		if err := s.Chart.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (t Table) validate() error {
	if len(t.Columns) == 0 || len(t.Columns) > maxColumns {
		return fmt.Errorf("a table needs 1 to %d columns", maxColumns)
	}
	for _, c := range t.Columns {
		if err := checkText("column title", c.Title, 40); err != nil {
			return err
		}
		switch c.Align {
		case "", "start", "end":
		default:
			return fmt.Errorf("column %q align must be start or end", c.Title)
		}
	}
	if len(t.Rows) > maxRows {
		return fmt.Errorf("more than %d table rows", maxRows)
	}
	for i, row := range t.Rows {
		if len(row) != len(t.Columns) {
			return fmt.Errorf("row %d has %d cells, want %d", i+1, len(row), len(t.Columns))
		}
		for _, cell := range row {
			if err := checkText("cell", cell, 200); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ch Chart) validate() error {
	switch ch.Kind {
	case "", "line", "bar":
	default:
		return fmt.Errorf("chart kind %q must be line or bar", ch.Kind)
	}
	if len(ch.Points) == 0 || len(ch.Points) > maxPoints {
		return fmt.Errorf("a chart needs 1 to %d points", maxPoints)
	}
	for _, p := range ch.Points {
		if err := checkText("chart label", p.Label, 24); err != nil {
			return err
		}
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			return errors.New("chart values must be finite numbers")
		}
	}
	return nil
}

// checkText bounds a string's length and refuses the control characters
// that could reorder or hide what a page shows.
func checkText(what, s string, max int) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s is not valid UTF-8", what)
	}
	if utf8.RuneCountInString(s) > max {
		return fmt.Errorf("%s is longer than %d characters", what, max)
	}
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", what)
		}
		if r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return fmt.Errorf("%s contains a bidirectional control character", what)
		}
	}
	return nil
}

// PlainText reports whether s is safe to show as one line of text, for
// callers that build strings outside the page model.
func PlainText(s string) bool {
	return checkText("text", s, 10_000) == nil
}

// Trim shortens s to at most max runes, for error messages.
func Trim(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
