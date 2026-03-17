package excel

import (
	"fmt"
	"report_redmine/internal/entities"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	redmineURL      = "https://redmine.bivgroup.com/issues/"
	colorRedFill    = "#FEC7CE"
	colorRedText    = "#9A0511"
	colorYellowFill = "#FFF2CC"
	colorYellowText = "#7F6000"
	dateFormat      = "02.01.06 15:04"
)

// Exporter предоставляет методы для экспорта задач в Excel
type Exporter struct{}

// New создаёт новый экземпляр Exporter
func New() *Exporter {
	return &Exporter{}
}

// Export создает Excel-файл и заполняет его данными из списка задач
func (e *Exporter) Export(issues []entities.Issue, filePath string) error {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println("Ошибка при закрытии файла:", err)
		}
	}()

	// Добавляем лист "Отчет"
	sheetName := "Отчет"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}

	// Удаляем лист по умолчанию
	if err = f.DeleteSheet("Sheet1"); err != nil {
		return err
	}

	// Активируем лист
	f.SetActiveSheet(index)

	// Заголовки
	headers := []string{
		"№ задачи", "Трекер", "Тема", "Приоритет", "Статус", "Дата создания",
		"Дата решения", "SLA (часы)", "Проект СБС", "Ссылка Jira", "Команда СБС",
		"SLA по договору (часы)", "Нарушение SLA (часы)", "Последний комментарий",
		"Автор последнего комментария", "Предыдущий статус", "Предыдущий приоритет",
		"Последнее изменение",
	}

	// Записываем заголовки
	for col, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		if err = f.SetCellValue(sheetName, cell, header); err != nil {
			return err
		}
	}

	err = f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,            // закрепляем 1 строку
		TopLeftCell: "A2",         // ячейка в левом верхнем углу после закрепления
		ActivePane:  "bottomLeft", // активная область - нижняя левая
		Selection: []excelize.Selection{
			{
				SQRef:      "A2",         // диапазон выделения
				ActiveCell: "A2",         // активная ячейка
				Pane:       "bottomLeft", // в какой области находится
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to set panes: %w", err)
	}

	if len(issues) > 0 {
		lastRow := len(issues) + 1
		lastCol, _ := excelize.ColumnNumberToName(len(headers))

		// Формируем диапазон (например, "A1:R100")
		rangeRef := fmt.Sprintf("A1:%s%d", lastCol, lastRow)

		// Применяем автофильтр (без предустановленных условий)
		err = f.AutoFilter(sheetName, rangeRef, []excelize.AutoFilterOptions{})
		if err != nil {
			return fmt.Errorf("failed to set auto filter: %w", err)
		}
	}

	// Заполняем строки с данными
	for i, issue := range issues {
		row := i + 2 // Начинаем со второй строки

		cellA := fmt.Sprintf("A%d", row)
		_ = f.SetCellValue(sheetName, cellA, issue.TaskNumber)
		_ = f.SetCellHyperLink(sheetName, cellA, redmineURL+strconv.Itoa(issue.TaskNumber), "External")

		_ = f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), issue.Tracker)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), issue.Theme)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), issue.Priority)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), issue.CurrentStatus)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), issue.CreateDate.Format(dateFormat))
		if !issue.ResolvedDate.IsZero() {
			_ = f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), issue.ResolvedDate.Format(dateFormat))
		} else {
			_ = f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), "")
		}
		_ = f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), issue.SLA)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), issue.SubprojectSBS)

		cellJ := fmt.Sprintf("J%d", row)
		_ = f.SetCellValue(sheetName, cellJ, issue.URLJiraSBS)
		_ = f.SetCellHyperLink(sheetName, cellJ, issue.URLJiraSBS, "External")

		_ = f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), issue.SBSTeams)
		if strings.Contains(strings.ToLower(issue.SubprojectSBS), "авто") &&
			strings.Contains(strings.ToLower(issue.Priority), "третий") {
			_ = f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), "По договоренности")
			_ = f.SetCellValue(sheetName, fmt.Sprintf("M%d", row), 0)
		} else {
			_ = f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), issue.DeadlineSLA)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("M%d", row), issue.MissingSLA)
		}

		_ = f.SetCellValue(sheetName, fmt.Sprintf("N%d", row), issue.LastComment)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("O%d", row), issue.LastCommentator)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("P%d", row), issue.PreviousStatus)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("Q%d", row), issue.PreviousPriority)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("R%d", row), issue.ModificationDate.Format(dateFormat))
	}

	lastCol, _ := excelize.ColumnNumberToName(len(headers))

	border := []excelize.Border{
		{
			Type:  "left",
			Color: "000000",
			Style: 1,
		},
		{
			Type:  "right",
			Color: "000000",
			Style: 1,
		},
		{
			Type:  "top",
			Color: "000000",
			Style: 1,
		},
		{
			Type:  "bottom",
			Color: "000000",
			Style: 1,
		},
	}

	//Стиль для линий между ячейками
	borderStyle, err := f.NewStyle(&excelize.Style{
		Border: border,
	})
	if err != nil {
		return fmt.Errorf("failed to create border style: %w", err)
	}

	// Применяем границы ко всей таблице с данными
	if len(issues) > 0 {
		lastRow := len(issues) + 1
		// Правильное формирование диапазона: от A1 до последней колонки и последней строки
		endCell := fmt.Sprintf("%s%d", lastCol, lastRow)
		err = f.SetCellStyle(sheetName, "A1", endCell, borderStyle)
		if err != nil {
			return fmt.Errorf("failed to set border style for data: %w", err)
		}
	}

	// Устанавливаем формат ячеек для чисел с запятой как разделителем
	floatStyle, err := f.NewStyle(&excelize.Style{
		NumFmt: 2,
		Border: border,
	})
	if err != nil {
		return fmt.Errorf("failed to create style: %w", err)
	}

	// Применяем стиль к столбцам H, L, M
	columns := []string{"H", "L", "M"}
	for _, col := range columns {
		if err = f.SetColStyle(sheetName, col, floatStyle); err != nil {
			return fmt.Errorf("failed to set column style for %s: %w", col, err)
		}
	}

	// Устанавливаем формат ячеек для даты
	dataStyle, err := f.NewStyle(&excelize.Style{
		CustomNumFmt: new("dd.mm.yy hh:mm"),
		Border:       border,
	})
	if err != nil {
		return fmt.Errorf("failed to create style: %w", err)
	}

	// Применяем стиль к столбцам F, G, R
	colum := []string{"F", "G", "R"}
	for _, col := range colum {
		if err = f.SetColStyle(sheetName, col, dataStyle); err != nil {
			return fmt.Errorf("failed to set column style for %s: %w", col, err)
		}
	}

	//Стиль текста для длинных значений
	longStyle, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
			WrapText:   true,
		},
		Border: border,
	})

	columnsLong := []string{"C", "N"}
	for _, col := range columnsLong {
		if err = f.SetColStyle(sheetName, col, longStyle); err != nil {
			return fmt.Errorf("failed to set column style for %s: %w", col, err)
		}
	}

	// Создаём стиль для гиперссылки: синий цвет и подчёркивание
	linkStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Color:     "0563C1",
			Underline: "single",
		},
		Border: border,
	})
	if err != nil {
		return fmt.Errorf("failed to create hyperlink style: %w", err)
	}

	urlColumns := []string{"A", "J"}
	for _, col := range urlColumns {
		if err = f.SetColStyle(sheetName, col, linkStyle); err != nil {
			return fmt.Errorf("failed to set column style for %s: %w", col, err)
		}
	}

	//Стиль текста для заголовков
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
		},
		Alignment: &excelize.Alignment{
			WrapText:   true,
			Vertical:   "center",
			Horizontal: "center",
		},
		Border: border,
	})
	if err != nil {
		return fmt.Errorf("failed to create header style: %w", err)
	}

	err = f.SetCellStyle(sheetName, "A1", lastCol+"1", headerStyle)
	if err != nil {
		return fmt.Errorf("failed to set header style: %w", err)
	}

	// Автоподбор ширины столбцов
	if err = e.autoSizeColumns(f, sheetName, len(headers)); err != nil {
		return fmt.Errorf("failed to autosize columns: %w", err)
	}

	// Создаём стиль для условного форматирования: значения > 0 — красным
	redCondStyle, err := f.NewConditionalStyle(
		&excelize.Style{
			Fill: excelize.Fill{
				Type:    "pattern",
				Color:   []string{colorRedFill},
				Pattern: 1,
			},
			Font: &excelize.Font{Color: colorRedText},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create conditional style: %w", err)
	}

	// Стиль для значений >= -4 и < 0 — жёлтый
	yellowCondStyle, err := f.NewConditionalStyle(
		&excelize.Style{
			Fill: excelize.Fill{
				Type:    "pattern",
				Color:   []string{colorYellowFill},
				Pattern: 1,
			},
			Font: &excelize.Font{Color: colorYellowText},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create yellow conditional style: %w", err)
	}

	lastRow := len(issues) + 1
	condRange := fmt.Sprintf("M2:M%d", lastRow)
	err = f.SetConditionalFormat(sheetName, condRange, []excelize.ConditionalFormatOptions{
		{
			Type:     "cell",
			Criteria: ">",
			Value:    "0",
			Format:   &redCondStyle,
		},
		{
			Type:     "cell",
			Criteria: "between",
			MinValue: "-4",
			MaxValue: "-0.1",
			Format:   &yellowCondStyle,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to set conditional format: %w", err)
	}

	// Сохраняем файл
	if err = f.SaveAs(filePath); err != nil {
		return fmt.Errorf("failed to save Excel file: %w", err)
	}

	return nil
}

// autoSizeColumns устанавливает ширину столбцов автоматически
func (e *Exporter) autoSizeColumns(f *excelize.File, sheetName string, colCount int) error {
	for col := 1; col <= colCount; col++ {
		width := 15.0
		switch col {
		case 3:
			width = 40
		case 14:
			width = 80
		case 4, 12, 15:
			width = 20
		case 9, 10, 11: // SBS поля
			width = 25
		}
		cell, _ := excelize.CoordinatesToCellName(col, 1)
		if err := f.SetColWidth(sheetName, cell[:1], cell[:1], width); err != nil {
			return err
		}
	}
	return nil
}
