// Package report writes a slice of model.Record as a .xlsx spreadsheet that
// opens in Excel, Google Sheets and LibreOffice.
package report

import (
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/gustavoaveiro/triagent/internal/model"
)

const sheetName = "Report"

var columns = []string{
	"Source Path", "Destination Path", "File Name", "Media Type", "Bucket",
	"Score", "Samples Checked", "Status", "Reason", "Size (bytes)", "Timestamp",
}

// excelFile is the subset of *excelize.File used to build the report. It
// exists so tests can inject a fake that fails on demand: every call below
// is made with fixed, valid arguments (a hardcoded sheet name, cell
// coordinates computed from bounded loop indices, a fixed style), so none
// of them can actually fail in production - the real library only returns
// an error here in response to programmer mistakes, which this interface
// lets tests simulate directly instead of leaving the check untested.
type excelFile interface {
	SetSheetName(source, target string) error
	SetCellStr(sheet, cell, value string) error
	SetCellValue(sheet, cell string, value interface{}) error
	NewStyle(style *excelize.Style) (int, error)
	SetCellStyle(sheet, topLeftCell, bottomRightCell string, styleID int) error
	AutoFilter(sheet, rangeRef string, opts []excelize.AutoFilterOptions) error
	SaveAs(name string, opts ...excelize.Options) error
	Close() error
}

// Write renders records into a new .xlsx file at path. Failed rows are
// highlighted red; rows moved to the Explicit bucket are highlighted
// amber, since those are the ones that actually changed the source tree.
func Write(path string, records []model.Record) error {
	return writeFile(excelize.NewFile(), path, records)
}

func writeFile(f excelFile, path string, records []model.Record) error {
	defer f.Close()

	if err := writeWorkbook(f, records); err != nil {
		return err
	}
	return f.SaveAs(path)
}

func writeWorkbook(f excelFile, records []model.Record) error {
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return err
	}

	for i, col := range columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheetName, cell, col); err != nil {
			return err
		}
	}

	failedStyle, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#F8CBAD"}, Pattern: 1},
	})
	if err != nil {
		return err
	}
	movedStyle, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFE699"}, Pattern: 1},
	})
	if err != nil {
		return err
	}

	for rowIdx, rec := range records {
		row := rowIdx + 2 // 1 header row, 1-indexed cells
		values := []interface{}{
			rec.SourcePath, rec.DestPath, rec.FileName, string(rec.MediaType),
			string(rec.Bucket), rec.Score, rec.SamplesChecked, string(rec.Status),
			rec.Reason, rec.SizeBytes, rec.Timestamp.Format("2006-01-02 15:04:05"),
		}
		for colIdx, v := range values {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, row)
			if err := f.SetCellValue(sheetName, cell, v); err != nil {
				return err
			}
		}

		var style int
		switch rec.Status {
		case model.StatusFailed:
			style = failedStyle
		case model.StatusMoved:
			style = movedStyle
		default:
			continue
		}
		firstCell, _ := excelize.CoordinatesToCellName(1, row)
		lastCell, _ := excelize.CoordinatesToCellName(len(columns), row)
		if err := f.SetCellStyle(sheetName, firstCell, lastCell, style); err != nil {
			return err
		}
	}

	return f.AutoFilter(sheetName, fmt.Sprintf("A1:%s1", mustCol(len(columns))), nil)
}

func mustCol(n int) string {
	col, _ := excelize.ColumnNumberToName(n)
	return col
}
