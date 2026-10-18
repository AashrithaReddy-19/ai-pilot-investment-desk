package docparse

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// sheetKeywords and sheetNames only help pick a sensible default sheet. Which rows count as AI spending
// is decided by the engine's own rules, never here.
var (
	sheetKeywords = regexp.MustCompile(`(?i)\b(ai|software|cloud|licen[cs]e|tokens?)\b`)
	sheetNames    = regexp.MustCompile(`(?i)balance|profit|loss|p\s*&\s*l|income|expens|statement|schedule|financ|cost|spend|trial|ledger`)
)

type sheetScore struct {
	usable bool
	score  int
}

func scoreSheet(sd sheetData) sheetScore {
	dataRows, hits := 0, 0
	for _, r := range sd.Rows {
		ri := analyse(r.Cells)
		if !ri.data {
			continue
		}
		dataRows++
		if ri.labelCol >= 0 && sheetKeywords.MatchString(r.Cells[ri.labelCol]) {
			hits++
		}
	}
	if dataRows == 0 {
		return sheetScore{}
	}
	s := dataRows + 3*hits
	if sheetNames.MatchString(sd.Name) {
		s += 5
	}
	if sd.Hidden {
		s -= 100000 // a hidden sheet is the last resort, never the default
	}
	return sheetScore{usable: true, score: s}
}

func convertSheets(ctx context.Context, format string, sheets []sheetData, opt Options) (*Result, error) {
	scores := make([]sheetScore, len(sheets))
	best := -1
	for i, sd := range sheets {
		scores[i] = scoreSheet(sd)
		if scores[i].usable && (best < 0 || scores[i].score > scores[best].score) {
			best = i
		}
	}
	chosen, explicit := best, strings.TrimSpace(opt.Sheet) != ""
	if explicit {
		chosen = -1
		for i, sd := range sheets {
			if strings.EqualFold(sd.Name, strings.TrimSpace(opt.Sheet)) {
				chosen = i
				break
			}
		}
		if chosen < 0 {
			names := make([]string, len(sheets))
			for i, sd := range sheets {
				names[i] = sd.Name
			}
			return nil, fail(CodeSheetNotFound, "sheet", "the workbook has no sheet named %q (sheets: %s)", opt.Sheet, strings.Join(names, ", "))
		}
	}
	if chosen < 0 {
		names := make([]string, len(sheets))
		for i, sd := range sheets {
			names[i] = sd.Name
		}
		return nil, fail(CodeNoTable, "sheet", "no sheet has a table of descriptions and amounts (sheets read: %s); check the file or pick another sheet", strings.Join(names, ", "))
	}

	sd := sheets[chosen]
	t, err := buildTable(sd.Rows, nil, opt)
	if err != nil {
		if e, ok := err.(*Error); ok && e.Code == CodeNoTable {
			e.Message = fmt.Sprintf("sheet %q: %s", sd.Name, e.Message)
			e.Field = "sheet"
		}
		return nil, err
	}
	res, err := finish(format, t, nil)
	if err != nil {
		return nil, err
	}
	res.Sheet = sd.Name
	for i, s := range sheets {
		res.Sheets = append(res.Sheets, SheetInfo{Name: s.Name, Hidden: s.Hidden, Rows: len(s.Rows), Usable: scores[i].usable, Suggested: i == best})
	}
	switch {
	case explicit:
		res.Notes = append(res.Notes, fmt.Sprintf("Sheet %q was read, as you chose.", sd.Name))
	case len(sheets) > 1:
		res.Notes = append(res.Notes, fmt.Sprintf("Sheet %q was read because it looks like the statement. The workbook has %d sheets; pick another if this is not the one.", sd.Name, len(sheets)))
	}
	if sd.Hidden {
		res.Notes = append(res.Notes, fmt.Sprintf("Sheet %q is hidden in the workbook.", sd.Name))
	}
	if sd.FormulaNoValue > 0 {
		res.Flags = append(res.Flags, Flag{Code: "FORMULA_NO_VALUE", Message: fmt.Sprintf("%d formula cell(s) on sheet %q have no stored result (the workbook was not recalculated before it was saved), so their amounts are blank. Open and save the workbook in Excel, then upload it again.", sd.FormulaNoValue, sd.Name)})
	}
	return res, nil
}
