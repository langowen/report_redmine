package excel

import (
	"fmt"
	"report_redmine/internal/entities"
	"strconv"

	"github.com/xuri/excelize/v2"
)

const (
	redmineURL = "https://redmine.bivgroup.com/issues/"
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
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return err
	}

	// Активируем лист
	f.SetActiveSheet(index)

	// Заголовки
	headers := []string{
		"№ задачи", "Трекер", "Тема", "Приоритет", "Статус", "Дата создания",
		"Дата решения", "SLA (часы)", "Проект SBS", "Ссылка Jira", "Команда SBS",
		"SLA по договору (часы)", "Нарушение SLA (часы)",
	}

	// Записываем заголовки
	for col, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		if err := f.SetCellValue(sheetName, cell, header); err != nil {
			return err
		}
	}

	// Устанавливаем формат ячеек для чисел с запятой как разделителем
	numberFormat := `0.00`
	style, err := f.NewStyle(&excelize.Style{
		CustomNumFmt: &numberFormat,
	})
	if err != nil {
		return fmt.Errorf("failed to create style: %w", err)
	}

	// Применяем стиль к столбцам H, L, M
	columns := []string{"H", "L", "M"}
	for _, col := range columns {
		if err = f.SetColStyle(sheetName, col, style); err != nil {
			return fmt.Errorf("failed to set column style for %s: %w", col, err)
		}
	}

	// Создаём стиль для гиперссылки: синий цвет и подчёркивание
	linkStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Color:     "0563C1",
			Underline: "single",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create hyperlink style: %w", err)
	}

	// Заполняем строки с данными
	for i, issue := range issues {
		row := i + 2 // Начинаем со второй строки

		cellA := fmt.Sprintf("A%d", row)
		_ = f.SetCellValue(sheetName, cellA, issue.TaskNumber)
		_ = f.SetCellHyperLink(sheetName, cellA, redmineURL+strconv.Itoa(issue.TaskNumber), "External")
		_ = f.SetCellStyle(sheetName, cellA, cellA, linkStyle)

		_ = f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), issue.Tracker)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), issue.Theme)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), issue.Priority)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), issue.CurrentStatus)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), issue.CreateDate.Format("02.01.2006 15:04"))
		if !issue.ResolvedDate.IsZero() {
			_ = f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), issue.ResolvedDate.Format("02.01.2006 15:04"))
		} else {
			_ = f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), "")
		}
		_ = f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), issue.SLA)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), valueOrEmpty(issue.SubprojectSBS))

		cellJ := fmt.Sprintf("J%d", row)
		_ = f.SetCellValue(sheetName, cellJ, valueOrEmpty(issue.URLJiraSBS))
		_ = f.SetCellHyperLink(sheetName, cellJ, valueOrEmpty(issue.URLJiraSBS), "External")
		_ = f.SetCellStyle(sheetName, cellJ, cellJ, linkStyle)

		_ = f.SetCellValue(sheetName, fmt.Sprintf("K%d", row), valueOrEmpty(issue.SBSTeams))
		_ = f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), issue.DeadlineSLA)
		_ = f.SetCellValue(sheetName, fmt.Sprintf("M%d", row), issue.MissingSLA)
	}

	// Автоподбор ширины столбцов
	if err = e.autoSizeColumns(f, sheetName, len(headers)); err != nil {
		return fmt.Errorf("failed to autosize columns: %w", err)
	}

	// Создаём стиль для условного форматирования: значения > 0 — красным
	condStyle, err := f.NewConditionalStyle(
		&excelize.Style{
			Fill: excelize.Fill{
				Type:    "pattern",
				Color:   []string{"#FEC7CE"},
				Pattern: 1,
			},
			Font: &excelize.Font{Color: "#9A0511"},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create conditional style: %w", err)
	}

	// Применяем условное форматирование КОНЕЦ заполнения данных
	lastRow := len(issues) + 1
	condRange := fmt.Sprintf("M2:M%d", lastRow)
	err = f.SetConditionalFormat(sheetName, condRange, []excelize.ConditionalFormatOptions{
		{
			Type:     "cell",
			Criteria: ">",
			Value:    "0",
			Format:   &condStyle, // Используем Style вместо Format
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

// valueOrEmpty возвращает строку или пустую строку, если указатель nil
func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// autoSizeColumns устанавливает ширину столбцов автоматически
func (e *Exporter) autoSizeColumns(f *excelize.File, sheetName string, colCount int) error {
	for col := 1; col <= colCount; col++ {
		width := 15.0
		switch col {
		case 3: // Тема
			width = 40
		case 8, 12, 13: // SLA и нарушения
			width = 14
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
