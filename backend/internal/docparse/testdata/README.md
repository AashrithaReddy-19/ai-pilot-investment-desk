# Test files

- `statement.xls`, `encrypted.xls`, `old_biff5.xls` were written for these tests by a small generator
  that follows the BIFF8 / OLE2 specification (synthetic figures only). `statement.xls` has four sheets
  (one hidden), RK numbers in every encoding (integer, integer/100, negative) and a custom number format;
  `encrypted.xls` carries a FILEPASS record; `old_biff5.xls` is marked as Excel 5.0 (BIFF5).
- `real_excel_table.xls` is the sample workbook `Table.xls` from the Apache-2.0 licensed module
  github.com/extrame/xls. It is a genuine Excel-written file (text only), used to check that the reader
  opens a file this project did not write.
- `real_statement_multipage.pdf`, `real_annual_report.pdf` and `real_statement_usd.pdf` are real PDFs
  printed by headless Chromium (embedded TrueType fonts, a heading repeated on every page, wrapped
  descriptions and column headings, page footers) from HTML made up for these tests. They stand in for
  the exported statements people upload and exercise font handling that hand-built PDFs cannot.

No file here contains real financial data.
