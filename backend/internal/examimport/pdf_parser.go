package examimport

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

type ParsedQuestion struct {
	Type        string
	Text        string
	Points      float64
	OrderIndex  int
	CorrectText string
	Options     []ParsedOption
}

type ParsedOption struct {
	Text      string
	IsCorrect bool
}

var (
	// Matches "Q1 [topic]" or "Q1." or "Q1)" or "Q1:".
	// Also matches plain "1." / "1)" as fallback.
	questionStartRe = regexp.MustCompile(
		`(?i)^\s*(?:Q\s*)?(\d{1,3})\s*(?:\[[^\]]*\]|[:\.\)])\s*(.*)$`)

	// Matches "● A)" / "• A)" / "A)" / "A." / "(A)" — with content.
	// Content may be empty (formulas stripped by PDF) — allow it.
	optionRe = regexp.MustCompile(
		`^\s*(?:[●•\-\*]\s*)?\(?([A-Ea-e])\)?\s*[\.\)\:]\s*(.*)$`)

	// Matches "ANSWER: C)" or "Answer: C" or "ANSWER: True".
	answerRe = regexp.MustCompile(
		`(?i)^\s*ANSWER\s*[:\-]\s*(.*)$`)

	// Matches "1." / "2." / "3." standalone numbered theory questions.
	theoryNumberRe = regexp.MustCompile(`^\s*(\d{1,3})\.\s+(.+)$`)

	// Section header we should skip.
	sectionHeaderRe = regexp.MustCompile(
		`(?i)^\s*(SECTION\s+[A-Z]|QUESTION\s+BANK|Course:|Target Application:|Formats Included:)`)
)

func ExtractText(r io.ReaderAt, size int64) (string, error) {
	doc, err := pdf.NewReader(r, size)
	if err != nil {
		return "", fmt.Errorf("opening PDF: %w", err)
	}
	var buf bytes.Buffer
	for i := 1; i <= doc.NumPage(); i++ {
		page := doc.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}

func ParseQuestions(text string) []ParsedQuestion {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Heuristic: some PDFs collapse newlines so multiple numbered
	// questions appear on a single long line. Help the parser by
	// ensuring a numeric question marker like "1." or "2." starts on
	// its own line when it is preceded by whitespace. This simple
	// transformation increases the chances of detecting multiple
	// questions in extracted text.

	// Insert newline before a numeric question marker when it's
	// collapsed into the previous token (e.g. "Answer: A2. The...").
	// Matches either whitespace+number (existing case) or a letter/paren
	// followed immediately by a number marker.
	numStartRe := regexp.MustCompile(`(?:\s|(\w|[\)\]\:]))(\d{1,3}\.)`)
	text = numStartRe.ReplaceAllString(text, "\n$2")

	// Some PDFs collapse multiple-choice options onto one line:
	// "A) Option OneB) Option TwoC) Option Three" — insert a newline
	// before each option letter when it's not already separated.
	optInlineRe := regexp.MustCompile(`([^\s])([A-Ea-e][\.|\)])`)
	text = optInlineRe.ReplaceAllString(text, "$1\n$2")

	lines := strings.Split(text, "\n")

	var questions []ParsedQuestion
	var current *ParsedQuestion
	orderIndex := 0

	flush := func() {
		if current == nil {
			return
		}
		finalizeQuestion(current)
		// Only keep questions that actually have content.
		if strings.TrimSpace(current.Text) != "" {
			questions = append(questions, *current)
		}
		current = nil
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		// Update mode on section headers.
		if sectionHeaderRe.MatchString(line) {
			// Section headers are recognized but not currently used to
			// influence parsing behavior. May be used for future enhancements.
			continue
		}

		// New question start?
		if m := questionStartRe.FindStringSubmatch(line); m != nil {
			// The "Q1 [topic]" pattern — m[2] is empty when the line was
			// just "Q1 [topic]" and the real question text is on the
			// next line. In that case, keep the question open and let
			// the next lines fill in Text.
			flush()
			orderIndex++
			current = &ParsedQuestion{
				Text:       strings.TrimSpace(m[2]),
				Points:     1,
				OrderIndex: orderIndex,
			}
			continue
		}

		// Standalone numbered theory questions ("1. Compare how...") when
		// no current question is open.
		if current == nil {
			if m := theoryNumberRe.FindStringSubmatch(line); m != nil {
				orderIndex++
				current = &ParsedQuestion{
					Text:       strings.TrimSpace(m[2]),
					Points:     1,
					OrderIndex: orderIndex,
				}
				continue
			}
			// Skip front matter.
			continue
		}

		// Option line?
		if m := optionRe.FindStringSubmatch(line); m != nil {
			current.Options = append(current.Options, ParsedOption{
				Text: strings.TrimSpace(m[2]),
			})
			continue
		}

		// Answer line?
		if m := answerRe.FindStringSubmatch(line); m != nil {
			applyAnswer(current, m[1])
			continue
		}

		// Explanation line — skip, don't append to question.
		if strings.HasPrefix(strings.ToLower(line), "explanation:") {
			continue
		}

		// Continuation of question text.
		current.Text = strings.TrimSpace(current.Text + " " + line)
	}
	flush()

	return questions
}

func applyAnswer(q *ParsedQuestion, answer string) {
	answer = strings.TrimSpace(answer)
	// Strip trailing ")" etc.
	answer = strings.TrimRight(answer, ").:;")
	lower := strings.ToLower(answer)

	// "C) To ensure thread safety..." → take just the letter.
	firstLetter := ""
	if len(answer) > 0 {
		firstLetter = strings.ToUpper(string(answer[0]))
	}

	if lower == "true" || lower == "false" {
		q.CorrectText = lower
		return
	}

	if len(answer) >= 1 && firstLetter >= "A" && firstLetter <= "E" {
		idx := int(firstLetter[0] - 'A')
		if idx >= 0 && idx < len(q.Options) {
			q.Options[idx].IsCorrect = true
			return
		}
	}

	if n, err := strconv.Atoi(answer); err == nil {
		if n >= 1 && n <= len(q.Options) {
			q.Options[n-1].IsCorrect = true
			return
		}
	}

	// Match full option text.
	for i, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Text), answer) {
			q.Options[i].IsCorrect = true
			return
		}
	}
}

func finalizeQuestion(q *ParsedQuestion) {
	// Detect T/F even without options: "ANSWER: TRUE" sets CorrectText.
	if q.CorrectText == "true" || q.CorrectText == "false" {
		q.Type = "TRUE_FALSE"
		return
	}
	if len(q.Options) > 0 {
		q.Type = "MULTIPLE_CHOICE"
		return
	}
	q.Type = "SHORT_ANSWER"
}
