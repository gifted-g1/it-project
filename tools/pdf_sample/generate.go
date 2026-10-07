package main

import (
	"log"
	"os"

	gofpdf "github.com/jung-kurt/gofpdf"
)

func main() {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetFont("Arial", "", 12)

	pdf.AddPage()
	pdf.CellFormat(0, 8, "1. What is an API?", "", 1, "", false, 0, "")
	pdf.CellFormat(0, 8, "A) Application Programming Interface", "", 1, "", false, 0, "")
	pdf.CellFormat(0, 8, "B) Automated Process Interface", "", 1, "", false, 0, "")
	pdf.CellFormat(0, 8, "C) Advanced Programming Instruction", "", 1, "", false, 0, "")
	pdf.CellFormat(0, 8, "D) Application Process Integration", "", 1, "", false, 0, "")
	pdf.Ln(4)
	pdf.CellFormat(0, 8, "Answer: A", "", 1, "", false, 0, "")
	pdf.Ln(6)

	pdf.CellFormat(0, 8, "2. The sky is blue during the day.", "", 1, "", false, 0, "")
	pdf.CellFormat(0, 8, "Answer: True", "", 1, "", false, 0, "")
	pdf.Ln(6)

	pdf.CellFormat(0, 8, "3. Explain polymorphism in OOP.", "", 1, "", false, 0, "")

	out := "sample_questions.pdf"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := pdf.OutputFileAndClose(out); err != nil {
		log.Fatalf("failed to write sample PDF: %v", err)
	}
	log.Printf("wrote %s", out)
}
